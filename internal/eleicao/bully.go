package eleicao

import (
	"encoding/json"
	"net"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"sync"
	"time"
)

// AlgoritmoBully implementa o algoritmo de eleição Bully
type AlgoritmoBully struct {
	idCorretor          string
	liderAtual          string
	vizinhos            map[string]*tipos.Vizinho
	emEleicao           bool
	canalEleicao        chan tipos.Mensagem
	canalResultado      chan string
	mutex               sync.RWMutex
	tempoEsperaResposta time.Duration
	tempoEsperaVitoria  time.Duration
}

// NovaEleicaoBully cria uma nova instância do algoritmo Bully
func NovaEleicaoBully(idCorretor string, vizinhos map[string]*tipos.Vizinho) *AlgoritmoBully {
	return &AlgoritmoBully{
		idCorretor:          idCorretor,
		liderAtual:          "",
		vizinhos:            vizinhos,
		emEleicao:           false,
		canalEleicao:        make(chan tipos.Mensagem, 100),
		canalResultado:      make(chan string, 10),
		tempoEsperaResposta: 3 * time.Second,
		tempoEsperaVitoria:  5 * time.Second,
	}
}

// IniciarEleicao inicia o processo de eleição
func (ab *AlgoritmoBully) IniciarEleicao() {
	ab.mutex.Lock()
	if ab.emEleicao {
		ab.mutex.Unlock()
		return
	}
	ab.emEleicao = true
	ab.mutex.Unlock()

	utils.RegistrarLog("INFO", "Corretor %s iniciando eleição", ab.idCorretor)

	// Encontra corretores com ID maior
	corretoresMaiores := ab.encontrarCorretoresMaiores()

	if len(corretoresMaiores) == 0 {
		// Nenhum corretor maior, este se torna líder
		ab.declararVitoria()
		return
	}

	// Envia mensagem de eleição para corretores maiores
	ab.enviarMensagensEleicao(corretoresMaiores)

	// Aguarda respostas
	select {
	case resposta := <-ab.canalEleicao:
		if resposta.Tipo == "RESPOSTA_ELEICAO" {
			utils.RegistrarLog("INFO", "Corretor %s recebeu resposta de eleição de %s",
				ab.idCorretor, resposta.OrigemID)
			ab.aguardarVitoria()
		}
	case <-time.After(ab.tempoEsperaResposta):
		// Timeout, declara vitória
		utils.RegistrarLog("INFO", "Corretor %s timeout aguardando respostas, declarando vitória", ab.idCorretor)
		ab.declararVitoria()
	}
}

// aguardarVitoria aguarda a declaração de vitória de um corretor maior
func (ab *AlgoritmoBully) aguardarVitoria() {
	utils.RegistrarLog("INFO", "Corretor %s aguardando anúncio de vitória", ab.idCorretor)

	// Aguarda mensagem de VITORIA ou timeout
	select {
	case msg := <-ab.canalEleicao:
		if msg.Tipo == "VITORIA" {
			utils.RegistrarLog("INFO", "Corretor %s recebeu anúncio de vitória de %s",
				ab.idCorretor, msg.OrigemID)
			ab.mutex.Lock()
			ab.emEleicao = false
			ab.mutex.Unlock()
		}
	case <-time.After(ab.tempoEsperaVitoria):
		// Timeout aguardando vitória, inicia nova eleição
		utils.RegistrarLog("AVISO", "Corretor %s timeout aguardando vitória, iniciando nova eleição", ab.idCorretor)
		ab.mutex.Lock()
		ab.emEleicao = false
		ab.mutex.Unlock()
		go ab.IniciarEleicao()
	}
}

// encontrarCorretoresMaiores retorna lista de corretores com ID maior
func (ab *AlgoritmoBully) encontrarCorretoresMaiores() []*tipos.Vizinho {
	var maiores []*tipos.Vizinho

	ab.mutex.RLock()
	defer ab.mutex.RUnlock()

	for _, vizinho := range ab.vizinhos {
		if vizinho.Ativo && vizinho.ID > ab.idCorretor {
			maiores = append(maiores, vizinho)
		}
	}

	return maiores
}

// enviarMensagensEleicao envia mensagens de eleição para corretores maiores
func (ab *AlgoritmoBully) enviarMensagensEleicao(corretores []*tipos.Vizinho) {
	mensagem := tipos.Mensagem{
		Tipo:         "ELEICAO",
		OrigemID:     ab.idCorretor,
		CarimboTempo: time.Now(),
	}

	for _, corretor := range corretores {
		go func(c *tipos.Vizinho) {
			if err := ab.enviarMensagemTCP(c.EnderecoTCP, mensagem); err != nil {
				utils.RegistrarLog("ERRO", "Falha ao enviar eleição para %s: %v", c.ID, err)
			}
		}(corretor)
	}
}

// declararVitoria declara este corretor como vencedor da eleição
func (ab *AlgoritmoBully) declararVitoria() {
	ab.mutex.Lock()
	ab.liderAtual = ab.idCorretor
	ab.emEleicao = false
	ab.mutex.Unlock()

	utils.RegistrarLog("INFO", "Corretor %s se declarou líder", ab.idCorretor)

	// Anuncia vitória para todos os vizinhos
	ab.anunciarVitoria()

	// Notifica resultado
	select {
	case ab.canalResultado <- ab.idCorretor:
	default:
	}
}

// anunciarVitoria anuncia vitória para todos os vizinhos
func (ab *AlgoritmoBully) anunciarVitoria() {
	mensagem := tipos.Mensagem{
		Tipo:         "VITORIA",
		OrigemID:     ab.idCorretor,
		Dados:        map[string]string{"lider": ab.idCorretor},
		CarimboTempo: time.Now(),
	}

	ab.mutex.RLock()
	defer ab.mutex.RUnlock()

	for _, vizinho := range ab.vizinhos {
		if vizinho.Ativo {
			go func(v *tipos.Vizinho) {
				if err := ab.enviarMensagemTCP(v.EnderecoTCP, mensagem); err != nil {
					utils.RegistrarLog("ERRO", "Falha ao anunciar vitória para %s: %v", v.ID, err)
				}
			}(vizinho)
		}
	}
}

// ProcessarMensagemEleicao processa mensagens relacionadas à eleição
func (ab *AlgoritmoBully) ProcessarMensagemEleicao(msg tipos.Mensagem) {
	switch msg.Tipo {
	case "ELEICAO":
		// Responde à mensagem de eleição
		resposta := tipos.Mensagem{
			Tipo:         "RESPOSTA_ELEICAO",
			OrigemID:     ab.idCorretor,
			DestinoID:    msg.OrigemID,
			CarimboTempo: time.Now(),
		}

		ab.mutex.RLock()
		vizinho, existe := ab.vizinhos[msg.OrigemID]
		ab.mutex.RUnlock()

		if existe {
			ab.enviarMensagemTCP(vizinho.EnderecoTCP, resposta)
		}

		// Inicia própria eleição se não estiver em uma
		ab.mutex.RLock()
		emEleicao := ab.emEleicao
		ab.mutex.RUnlock()

		if !emEleicao {
			go ab.IniciarEleicao()
		}

	case "RESPOSTA_ELEICAO":
		// Encaminha resposta para o canal
		select {
		case ab.canalEleicao <- msg:
		default:
			utils.RegistrarLog("AVISO", "Canal de eleição cheio, descartando mensagem de %s", msg.OrigemID)
		}

	case "VITORIA":
		// Atualiza líder
		if dados, ok := msg.Dados.(map[string]interface{}); ok {
			if lider, existe := dados["lider"]; existe {
				liderStr := lider.(string)

				ab.mutex.Lock()
				ab.liderAtual = liderStr
				ab.emEleicao = false
				ab.mutex.Unlock()

				utils.RegistrarLog("INFO", "Corretor %s reconhece %s como líder",
					ab.idCorretor, liderStr)

				// Encaminha para o canal de eleição também
				select {
				case ab.canalEleicao <- msg:
				default:
				}

				select {
				case ab.canalResultado <- liderStr:
				default:
				}
			}
		}
	}
}

// enviarMensagemTCP envia uma mensagem via TCP
func (ab *AlgoritmoBully) enviarMensagemTCP(endereco string, msg tipos.Mensagem) error {
	conexao, err := net.DialTimeout("tcp", endereco, 5*time.Second)
	if err != nil {
		return err
	}
	defer conexao.Close()

	dados, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = conexao.Write(append(dados, '\n'))
	return err
}

// ObterLiderAtual retorna o líder atual
func (ab *AlgoritmoBully) ObterLiderAtual() string {
	ab.mutex.RLock()
	defer ab.mutex.RUnlock()
	return ab.liderAtual
}

// ObterCanalResultado retorna o canal de resultados
func (ab *AlgoritmoBully) ObterCanalResultado() <-chan string {
	return ab.canalResultado
}

// EstaEmEleicao retorna se o corretor está participando de uma eleição
func (ab *AlgoritmoBully) EstaEmEleicao() bool {
	ab.mutex.RLock()
	defer ab.mutex.RUnlock()
	return ab.emEleicao
}

// AtualizarVizinhos atualiza a lista de vizinhos
func (ab *AlgoritmoBully) AtualizarVizinhos(vizinhos map[string]*tipos.Vizinho) {
	ab.mutex.Lock()
	defer ab.mutex.Unlock()
	ab.vizinhos = vizinhos
}
