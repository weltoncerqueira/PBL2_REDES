package gossip

import (
	"context"
	"encoding/json"
	"net"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"sync"
	"sync/atomic"
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
	executando         atomic.Bool // NOVO: Usar atomic para thread-safety
	ctx                context.Context
	cancel             context.CancelFunc
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

	ctx, cancel := context.WithCancel(context.Background())

	gb := &GerenciadorBatimentos{
		idCorretor:         idCorretor,
		vizinhos:           vizinhos,
		conexaoUDP:         conexao,
		intervaloBatimento: 2 * time.Second,
		tempoLimiteFalha:   6 * time.Second,
		canalFalha:         make(chan string, 10),
		ctx:                ctx,
		cancel:             cancel,
	}

	gb.executando.Store(true)

	return gb, nil
}

// Iniciar inicia o envio e recebimento de batimentos
func (gb *GerenciadorBatimentos) Iniciar() {
	go gb.enviarBatimentos()
	go gb.receberBatimentos()
	go gb.verificarFalhas()

	utils.RegistrarLog("INFO", "Gerenciador de batimentos iniciado para corretor %s", gb.idCorretor)
}

// enviarBatimentos envia periodicamente batimentos para vizinhos
// CORRIGIDO: Melhorado com context, tratamento seguro de vizinhos e recovery
func (gb *GerenciadorBatimentos) enviarBatimentos() {
	defer func() {
		if r := recover(); r != nil {
			utils.RegistrarLog("ERRO", "Panic em enviarBatimentos: %v", r)
		}
	}()

	ticker := time.NewTicker(gb.intervaloBatimento)
	defer ticker.Stop()

	for {
		select {
		case <-gb.ctx.Done():
			utils.RegistrarLog("INFO", "enviarBatimentos cancelado")
			return
		case <-ticker.C:
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

			// CORRIGIDO: Cópia segura de vizinhos para evitar lock prolongado
			gb.mutex.RLock()
			vizinhosAtivos := make([]*tipos.Vizinho, 0)
			for _, vizinho := range gb.vizinhos {
				if vizinho.Ativo {
					v := *vizinho // Cópia
					vizinhosAtivos = append(vizinhosAtivos, &v)
				}
			}
			gb.mutex.RUnlock()

			// Envia para vizinhos fora do lock
			for _, vizinho := range vizinhosAtivos {
				go gb.enviarBatimentoParaVizinho(dados, vizinho)
			}
		}
	}
}

// enviarBatimentoParaVizinho envia batimento para um vizinho específico
// NOVO: Função auxiliar com tratamento seguro
func (gb *GerenciadorBatimentos) enviarBatimentoParaVizinho(dados []byte, vizinho *tipos.Vizinho) {
	defer func() {
		if r := recover(); r != nil {
			utils.RegistrarLog("ERRO", "Panic ao enviar batimento para %s: %v", vizinho.ID, r)
		}
	}()

	endereco, err := net.ResolveUDPAddr("udp", vizinho.EnderecoUDP)
	if err != nil {
		utils.RegistrarLog("ERRO", "Endereço UDP inválido %s: %v", vizinho.EnderecoUDP, err)
		return
	}

	_, err = gb.conexaoUDP.WriteToUDP(dados, endereco)
	if err != nil {
		utils.RegistrarLog("AVISO", "Falha ao enviar batimento para %s: %v", vizinho.ID, err)
	}
}

// receberBatimentos recebe batimentos de outros corretores
// CORRIGIDO: Adicionado context e recovery
func (gb *GerenciadorBatimentos) receberBatimentos() {
	defer func() {
		if r := recover(); r != nil {
			utils.RegistrarLog("ERRO", "Panic em receberBatimentos: %v", r)
		}
	}()

	buffer := make([]byte, 1024)

	for gb.executando.Load() {
		select {
		case <-gb.ctx.Done():
			utils.RegistrarLog("INFO", "receberBatimentos cancelado")
			return
		default:
		}

		gb.conexaoUDP.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, _, err := gb.conexaoUDP.ReadFromUDP(buffer)

		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			if gb.executando.Load() {
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
// CORRIGIDO: Atualiza a estrutura corretamente sem causar race condition
func (gb *GerenciadorBatimentos) processarBatimento(batimento tipos.Mensagem) {
	gb.mutex.Lock()
	defer gb.mutex.Unlock()

	if vizinho, existe := gb.vizinhos[batimento.OrigemID]; existe {
		// CORRIGIDO: Atualizar tempo de batimento
		vizinho.UltimoBatimento = time.Now()

		// CORRIGIDO: Atualizar map com a nova referência
		if !vizinho.Ativo {
			vizinho.Ativo = true
			utils.RegistrarLog("INFO", "Corretor %s voltou a ficar ativo", batimento.OrigemID)
		}

		gb.vizinhos[batimento.OrigemID] = vizinho
	}
}

// verificarFalhas verifica periodicamente por falhas em vizinhos
// CORRIGIDO: Adicionado context, recovery e atualização segura de estado
func (gb *GerenciadorBatimentos) verificarFalhas() {
	defer func() {
		if r := recover(); r != nil {
			utils.RegistrarLog("ERRO", "Panic em verificarFalhas: %v", r)
		}
	}()

	ticker := time.NewTicker(gb.intervaloBatimento)
	defer ticker.Stop()

	for {
		select {
		case <-gb.ctx.Done():
			utils.RegistrarLog("INFO", "verificarFalhas cancelado")
			return
		case <-ticker.C:
			agora := time.Now()
			gb.mutex.Lock()

			for id, vizinho := range gb.vizinhos {
				if vizinho.Ativo && agora.Sub(vizinho.UltimoBatimento) > gb.tempoLimiteFalha {
					vizinho.Ativo = false
					gb.vizinhos[id] = vizinho

					utils.RegistrarLog("AVISO", "Corretor %s detectado como falho", id)

					// CORRIGIDO: Melhorado logging de tentativa de envio no canal
					select {
					case gb.canalFalha <- id:
						utils.RegistrarLog("INFO", "Falha do corretor %s notificada", id)
					default:
						utils.RegistrarLog("AVISO", "Canal de falha cheio, descartando notificação de %s", id)
					}
				}
			}

			gb.mutex.Unlock()
		}
	}
}

// Parar interrompe o gerenciador de batimentos
// CORRIGIDO: Melhorado processo de parada com sincronização
func (gb *GerenciadorBatimentos) Parar() {
	// Marca como não executando
	gb.executando.Store(false)

	// Cancela contexto
	gb.cancel()

	// Aguarda um pouco para goroutines terminarem
	time.Sleep(100 * time.Millisecond)

	// Fecha conexão UDP
	if gb.conexaoUDP != nil {
		if err := gb.conexaoUDP.Close(); err != nil {
			utils.RegistrarLog("AVISO", "Erro ao fechar conexão UDP: %v", err)
		}
	}

	// Fecha canal de falha
	close(gb.canalFalha)

	utils.RegistrarLog("INFO", "Gerenciador de batimentos parado para corretor %s", gb.idCorretor)
}

// ObterCanalFalha retorna o canal de notificação de falhas
func (gb *GerenciadorBatimentos) ObterCanalFalha() <-chan string {
	return gb.canalFalha
}
