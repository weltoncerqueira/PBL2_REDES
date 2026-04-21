package gossip

import (
	"encoding/json"
	"net"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"sync"
	"time"
)

// GerenciadorBatimentos gerencia batimentos cardíacos entre corretores
type GerenciadorBatimentos struct {
	idCorretor         string
	vizinhos           map[string]*tipos.Vizinho
	conexaoUDP         *net.UDPConn
	intervaloBatimento time.Duration
	tempoLimiteFalha   time.Duration
	canalFalha         chan string
	mutex              sync.RWMutex
	executando         bool
}

// NovoGerenciadorBatimentos cria um novo gerenciador de batimentos
func NovoGerenciadorBatimentos(idCorretor string, vizinhos map[string]*tipos.Vizinho,
	portaUDP string) (*GerenciadorBatimentos, error) {

	endereco, err := net.ResolveUDPAddr("udp", portaUDP)
	if err != nil {
		return nil, err
	}

	conexao, err := net.ListenUDP("udp", endereco)
	if err != nil {
		return nil, err
	}

	return &GerenciadorBatimentos{
		idCorretor:         idCorretor,
		vizinhos:           vizinhos,
		conexaoUDP:         conexao,
		intervaloBatimento: 2 * time.Second,
		tempoLimiteFalha:   6 * time.Second,
		canalFalha:         make(chan string, 10),
		executando:         true,
	}, nil
}

// Iniciar inicia o envio e recebimento de batimentos
func (gb *GerenciadorBatimentos) Iniciar() {
	go gb.enviarBatimentos()
	go gb.receberBatimentos()
	go gb.verificarFalhas()

	utils.RegistrarLog("INFO", "Gerenciador de batimentos iniciado para corretor %s", gb.idCorretor)
}

// enviarBatimentos envia periodicamente batimentos para vizinhos
func (gb *GerenciadorBatimentos) enviarBatimentos() {
	ticker := time.NewTicker(gb.intervaloBatimento)
	defer ticker.Stop()

	for gb.executando {
		<-ticker.C

		batimento := tipos.Mensagem{
			Tipo:         "BATIMENTO",
			OrigemID:     gb.idCorretor,
			CarimboTempo: time.Now(),
		}

		dados, err := json.Marshal(batimento)
		if err != nil {
			utils.RegistrarLog("ERRO", "Falha ao serializar batimento: %v", err)
			continue
		}

		gb.mutex.RLock()
		for _, vizinho := range gb.vizinhos {
			if !vizinho.Ativo {
				continue
			}

			go func(v *tipos.Vizinho) {
				endereco, err := net.ResolveUDPAddr("udp", v.EnderecoUDP)
				if err != nil {
					utils.RegistrarLog("ERRO", "Endereço UDP inválido %s: %v", v.EnderecoUDP, err)
					return
				}

				_, err = gb.conexaoUDP.WriteToUDP(dados, endereco)
				if err != nil {
					utils.RegistrarLog("AVISO", "Falha ao enviar batimento para %s: %v", v.ID, err)
				}
			}(vizinho)
		}
		gb.mutex.RUnlock()
	}
}

// receberBatimentos recebe batimentos de outros corretores
func (gb *GerenciadorBatimentos) receberBatimentos() {
	buffer := make([]byte, 1024)

	for gb.executando {
		gb.conexaoUDP.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, _, err := gb.conexaoUDP.ReadFromUDP(buffer)

		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if gb.executando {
				utils.RegistrarLog("ERRO", "Erro ao receber batimento: %v", err)
			}
			continue
		}

		var batimento tipos.Mensagem
		if err := json.Unmarshal(buffer[:n], &batimento); err != nil {
			utils.RegistrarLog("ERRO", "Falha ao desserializar batimento: %v", err)
			continue
		}

		if batimento.Tipo == "BATIMENTO" {
			gb.processarBatimento(batimento)
		}
	}
}

// processarBatimento processa um batimento recebido
func (gb *GerenciadorBatimentos) processarBatimento(batimento tipos.Mensagem) {
	gb.mutex.Lock()
	defer gb.mutex.Unlock()

	if vizinho, existe := gb.vizinhos[batimento.OrigemID]; existe {
		vizinho.UltimoBatimento = time.Now()
		if !vizinho.Ativo {
			vizinho.Ativo = true
			utils.RegistrarLog("INFO", "Corretor %s voltou a ficar ativo", batimento.OrigemID)
		}
	}
}

// verificarFalhas verifica periodicamente por falhas em vizinhos
func (gb *GerenciadorBatimentos) verificarFalhas() {
	ticker := time.NewTicker(gb.intervaloBatimento)
	defer ticker.Stop()

	for gb.executando {
		<-ticker.C

		agora := time.Now()
		gb.mutex.Lock()

		for id, vizinho := range gb.vizinhos {
			if vizinho.Ativo && agora.Sub(vizinho.UltimoBatimento) > gb.tempoLimiteFalha {
				vizinho.Ativo = false
				utils.RegistrarLog("AVISO", "Corretor %s detectado como falho", id)

				// Notifica falha
				select {
				case gb.canalFalha <- id:
				default:
				}
			}
		}

		gb.mutex.Unlock()
	}
}

// Parar interrompe o gerenciador de batimentos
func (gb *GerenciadorBatimentos) Parar() {
	gb.executando = false
	if gb.conexaoUDP != nil {
		gb.conexaoUDP.Close()
	}
}

// ObterCanalFalha retorna o canal de notificação de falhas
func (gb *GerenciadorBatimentos) ObterCanalFalha() <-chan string {
	return gb.canalFalha
}
