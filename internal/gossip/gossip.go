package gossip

import (
	"encoding/json"
	"net"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"sync"
	"time"
)

// ProtocoloGossip implementa o protocolo de gossip para disseminação de estado
type ProtocoloGossip struct {
	idCorretor       string
	estado           *tipos.EstadoCorretor
	vizinhos         map[string]*tipos.Vizinho
	intervaloGossip  time.Duration
	mutex            sync.RWMutex
	executando       bool
	canalAtualizacao chan *tipos.EstadoCorretor
}

// NovoProtocoloGossip cria uma nova instância do protocolo gossip
func NovoProtocoloGossip(idCorretor string, estado *tipos.EstadoCorretor,
	vizinhos map[string]*tipos.Vizinho) *ProtocoloGossip {

	return &ProtocoloGossip{
		idCorretor:       idCorretor,
		estado:           estado,
		vizinhos:         vizinhos,
		intervaloGossip:  5 * time.Second,
		executando:       true,
		canalAtualizacao: make(chan *tipos.EstadoCorretor, 10),
	}
}

// Iniciar inicia o protocolo gossip
func (pg *ProtocoloGossip) Iniciar() {
	go pg.disseminarEstado()
	go pg.receberAtualizacoes()

	utils.RegistrarLog("INFO", "Protocolo gossip iniciado para corretor %s", pg.idCorretor)
}

// disseminarEstado dissemina periodicamente o estado para vizinhos aleatórios
func (pg *ProtocoloGossip) disseminarEstado() {
	ticker := time.NewTicker(pg.intervaloGossip)
	defer ticker.Stop()

	for pg.executando {
		<-ticker.C

		pg.mutex.RLock()
		vizinhosAtivos := pg.obterVizinhosAtivos()
		pg.mutex.RUnlock()

		if len(vizinhosAtivos) == 0 {
			continue
		}

		// Seleciona até 3 vizinhos aleatórios
		selecionados := pg.selecionarVizinhosAleatorios(vizinhosAtivos, 3)

		mensagem := tipos.Mensagem{
			Tipo:         "GOSSIP",
			OrigemID:     pg.idCorretor,
			Dados:        pg.estado,
			CarimboTempo: time.Now(),
		}

		for _, vizinho := range selecionados {
			go func(v *tipos.Vizinho) {
				pg.enviarEstadoUDP(v.EnderecoUDP, mensagem)
			}(vizinho)
		}
	}
}

// receberAtualizacoes processa atualizações de estado recebidas
func (pg *ProtocoloGossip) receberAtualizacoes() {
	for pg.executando {
		select {
		case novoEstado := <-pg.canalAtualizacao:
			pg.mesclarEstado(novoEstado)
		}
	}
}

// ProcessarMensagemGossip processa uma mensagem gossip recebida
func (pg *ProtocoloGossip) ProcessarMensagemGossip(msg tipos.Mensagem) {
	if msg.Tipo != "GOSSIP" {
		return
	}

	if estadoRecebido, ok := msg.Dados.(*tipos.EstadoCorretor); ok {
		if estadoRecebido.Versao > pg.estado.Versao {
			select {
			case pg.canalAtualizacao <- estadoRecebido:
			default:
			}
		}
	}
}

// mesclarEstado mescla um estado recebido com o estado local
func (pg *ProtocoloGossip) mesclarEstado(novoEstado *tipos.EstadoCorretor) {
	pg.mutex.Lock()
	defer pg.mutex.Unlock()

	if novoEstado.Versao <= pg.estado.Versao {
		return
	}

	// Atualiza recursos
	for id, recurso := range novoEstado.Recursos {
		if recursoLocal, existe := pg.estado.Recursos[id]; !existe ||
			recurso.Versao > recursoLocal.Versao {
			pg.estado.Recursos[id] = recurso
		}
	}

	// Atualiza informações de vizinhos
	for id, vizinho := range novoEstado.Vizinhos {
		if id != pg.idCorretor {
			if vizinhoLocal, existe := pg.estado.Vizinhos[id]; !existe ||
				vizinho.VersaoEstado > vizinhoLocal.VersaoEstado {
				pg.estado.Vizinhos[id] = vizinho
			}
		}
	}

	pg.estado.Versao = novoEstado.Versao
	pg.estado.UltimaAtualizacao = time.Now()

	utils.RegistrarLog("INFO", "Estado atualizado para versão %d", pg.estado.Versao)
}

// obterVizinhosAtivos retorna lista de vizinhos ativos
func (pg *ProtocoloGossip) obterVizinhosAtivos() []*tipos.Vizinho {
	var ativos []*tipos.Vizinho

	for _, vizinho := range pg.vizinhos {
		if vizinho.Ativo {
			ativos = append(ativos, vizinho)
		}
	}

	return ativos
}

// selecionarVizinhosAleatorios seleciona n vizinhos aleatórios
func (pg *ProtocoloGossip) selecionarVizinhosAleatorios(vizinhos []*tipos.Vizinho, n int) []*tipos.Vizinho {
	if len(vizinhos) <= n {
		return vizinhos
	}

	// Implementação simples de seleção aleatória
	selecionados := make([]*tipos.Vizinho, n)
	permutacao := time.Now().UnixNano()

	for i := 0; i < n; i++ {
		indice := int(permutacao+int64(i)) % len(vizinhos)
		selecionados[i] = vizinhos[indice]
	}

	return selecionados
}

// enviarEstadoUDP envia estado via UDP para um vizinho
func (pg *ProtocoloGossip) enviarEstadoUDP(endereco string, msg tipos.Mensagem) error {
	enderecoUDP, err := net.ResolveUDPAddr("udp", endereco)
	if err != nil {
		return err
	}

	conexao, err := net.DialUDP("udp", nil, enderecoUDP)
	if err != nil {
		return err
	}
	defer conexao.Close()

	dados, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	_, err = conexao.Write(dados)
	return err
}

// Parar interrompe o protocolo gossip
func (pg *ProtocoloGossip) Parar() {
	pg.executando = false
}
