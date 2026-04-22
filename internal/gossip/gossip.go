package gossip

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"sync"
	"sync/atomic"
	"time"
)

// ProtocoloGossip implementa o protocolo de gossip para disseminação de estado
type ProtocoloGossip struct {
	idCorretor       string
	estado           *tipos.EstadoCorretor
	vizinhos         map[string]*tipos.Vizinho
	intervaloGossip  time.Duration
	mutex            sync.RWMutex
	executando       atomic.Bool // NOVO: Usar atomic para thread-safety
	canalAtualizacao chan *tipos.EstadoCorretor
	ctx              context.Context    // NOVO: Para cancelamento
	cancel           context.CancelFunc // NOVO: Para cancelamento
}

// NovoProtocoloGossip cria uma nova instância do protocolo gossip
func NovoProtocoloGossip(idCorretor string, estado *tipos.EstadoCorretor,
	vizinhos map[string]*tipos.Vizinho) *ProtocoloGossip {

	ctx, cancel := context.WithCancel(context.Background())

	pg := &ProtocoloGossip{
		idCorretor:       idCorretor,
		estado:           estado,
		vizinhos:         vizinhos,
		intervaloGossip:  5 * time.Second,
		canalAtualizacao: make(chan *tipos.EstadoCorretor, 10),
		ctx:              ctx,
		cancel:           cancel,
	}

	pg.executando.Store(true)

	return pg
}

// Iniciar inicia o protocolo gossip
func (pg *ProtocoloGossip) Iniciar() {
	go pg.disseminarEstado()
	go pg.receberAtualizacoes()

	utils.RegistrarLog("INFO", "Protocolo gossip iniciado para corretor %s", pg.idCorretor)
}

// disseminarEstado dissemina periodicamente o estado para vizinhos aleatórios
// CORRIGIDO: Adicionado context, recovery e cópia segura de estado
func (pg *ProtocoloGossip) disseminarEstado() {
	defer func() {
		if r := recover(); r != nil {
			utils.RegistrarLog("ERRO", "Panic em disseminarEstado: %v", r)
		}
	}()

	ticker := time.NewTicker(pg.intervaloGossip)
	defer ticker.Stop()

	for {
		select {
		case <-pg.ctx.Done():
			utils.RegistrarLog("INFO", "disseminarEstado cancelado")
			return
		case <-ticker.C:
			pg.mutex.RLock()
			vizinhosAtivos := pg.obterVizinhosAtivos()
			// CORRIGIDO: Fazer cópia do estado para evitar lock prolongado
			estadoCopia := pg.copiarEstado(pg.estado)
			pg.mutex.RUnlock()

			if len(vizinhosAtivos) == 0 {
				continue
			}

			// Seleciona até 3 vizinhos aleatórios
			selecionados := pg.selecionarVizinhosAleatorios(vizinhosAtivos, 3)

			mensagem := tipos.Mensagem{
				Tipo:         "GOSSIP",
				OrigemID:     pg.idCorretor,
				Dados:        estadoCopia,
				CarimboTempo: time.Now(),
			}

			for _, vizinho := range selecionados {
				go pg.enviarEstadoParaVizinho(vizinho.EnderecoUDP, mensagem)
			}
		}
	}
}

// receberAtualizacoes processa atualizações de estado recebidas
// CORRIGIDO: Adicionado context e recovery
func (pg *ProtocoloGossip) receberAtualizacoes() {
	defer func() {
		if r := recover(); r != nil {
			utils.RegistrarLog("ERRO", "Panic em receberAtualizacoes: %v", r)
		}
	}()

	for {
		select {
		case <-pg.ctx.Done():
			utils.RegistrarLog("INFO", "receberAtualizacoes cancelado")
			return
		case novoEstado, aberto := <-pg.canalAtualizacao:
			if !aberto {
				return
			}

			if novoEstado != nil {
				pg.mesclarEstado(novoEstado)
			}
		}
	}
}

// ProcessarMensagemGossip processa uma mensagem gossip recebida
// CORRIGIDO: Conversão segura de tipos com melhor tratamento de erros
func (pg *ProtocoloGossip) ProcessarMensagemGossip(msg tipos.Mensagem) {
	if msg.Tipo != "GOSSIP" {
		return
	}

	// CORRIGIDO: Conversão segura de dados para EstadoCorretor
	estadoRecebido := pg.converterParaEstado(msg.Dados)
	if estadoRecebido == nil {
		utils.RegistrarLog("AVISO", "Não foi possível converter dados gossip de %s", msg.OrigemID)
		return
	}

	pg.mutex.RLock()
	versaoAtual := pg.estado.Versao
	pg.mutex.RUnlock()

	if estadoRecebido.Versao > versaoAtual {
		select {
		case pg.canalAtualizacao <- estadoRecebido:
			utils.RegistrarLog("INFO", "Atualização de estado enfileirada de %s (versão %d)",
				msg.OrigemID, estadoRecebido.Versao)
		default:
			utils.RegistrarLog("AVISO", "Canal de atualização cheio, descartando mensagem de %s", msg.OrigemID)
		}
	}
}

// converterParaEstado converte dados para EstadoCorretor de forma segura
// NOVO: Função para conversão segura de tipos
func (pg *ProtocoloGossip) converterParaEstado(dados interface{}) *tipos.EstadoCorretor {
	switch v := dados.(type) {
	case *tipos.EstadoCorretor:
		return v
	case tipos.EstadoCorretor:
		return &v
	case map[string]interface{}:
		// Tentar converter do mapa
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			utils.RegistrarLog("ERRO", "Erro ao serializar dados do mapa: %v", err)
			return nil
		}
		var estado tipos.EstadoCorretor
		if err := json.Unmarshal(jsonBytes, &estado); err != nil {
			utils.RegistrarLog("ERRO", "Erro ao desserializar estado: %v", err)
			return nil
		}
		return &estado
	default:
		utils.RegistrarLog("ERRO", "Tipo de dados desconhecido para estado: %T", dados)
		return nil
	}
}

// mesclarEstado mescla um estado recebido com o estado local
// CORRIGIDO: Melhor sincronização e validação
func (pg *ProtocoloGossip) mesclarEstado(novoEstado *tipos.EstadoCorretor) {
	if novoEstado == nil {
		return
	}

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

	utils.RegistrarLog("INFO", "Estado mesclado com sucesso - nova versão: %d", pg.estado.Versao)
}

// copiarEstado faz uma cópia profunda do estado
// NOVO: Função para cópia segura de estado
func (pg *ProtocoloGossip) copiarEstado(estado *tipos.EstadoCorretor) *tipos.EstadoCorretor {
	if estado == nil {
		return nil
	}

	copia := &tipos.EstadoCorretor{
		ID:                estado.ID,
		LiderAtual:        estado.LiderAtual,
		Vizinhos:          make(map[string]tipos.Vizinho),
		Recursos:          make(map[string]tipos.Recurso),
		UltimaAtualizacao: estado.UltimaAtualizacao,
		Versao:            estado.Versao,
	}

	for k, v := range estado.Vizinhos {
		copia.Vizinhos[k] = v
	}

	for k, v := range estado.Recursos {
		copia.Recursos[k] = v
	}

	return copia
}

// obterVizinhosAtivos retorna lista de vizinhos ativos
// CORRIGIDO: Fazer cópia segura dos vizinhos
func (pg *ProtocoloGossip) obterVizinhosAtivos() []*tipos.Vizinho {
	var ativos []*tipos.Vizinho

	for _, vizinho := range pg.vizinhos {
		if vizinho != nil && vizinho.Ativo {
			v := *vizinho // Cópia
			ativos = append(ativos, &v)
		}
	}

	return ativos
}

// selecionarVizinhosAleatorios seleciona n vizinhos aleatórios
// CORRIGIDO: Usar rand.Perm para seleção verdadeiramente aleatória
func (pg *ProtocoloGossip) selecionarVizinhosAleatorios(vizinhos []*tipos.Vizinho, n int) []*tipos.Vizinho {
	if len(vizinhos) <= n {
		return vizinhos
	}

	// CORRIGIDO: Implementação adequada de seleção aleatória
	selecionados := make([]*tipos.Vizinho, 0, n)

	// Seed para aleatoriedade melhorada
	rand.Seed(time.Now().UnixNano())

	// Usar índices aleatórios sem repetição
	indices := rand.Perm(len(vizinhos))

	for i := 0; i < n && i < len(indices); i++ {
		selecionados = append(selecionados, vizinhos[indices[i]])
	}

	return selecionados
}

// enviarEstadoParaVizinho envia estado para um vizinho específico
// NOVO: Função auxiliar com tratamento seguro de erros
func (pg *ProtocoloGossip) enviarEstadoParaVizinho(endereco string, msg tipos.Mensagem) {
	defer func() {
		if r := recover(); r != nil {
			utils.RegistrarLog("ERRO", "Panic ao enviar estado para %s: %v", endereco, r)
		}
	}()

	if err := pg.enviarEstadoUDP(endereco, msg); err != nil {
		utils.RegistrarLog("AVISO", "Falha ao enviar estado gossip para %s: %v", endereco, err)
	}
}

// enviarEstadoUDP envia estado via UDP para um vizinho
// CORRIGIDO: Melhor tratamento de erros e logging
func (pg *ProtocoloGossip) enviarEstadoUDP(endereco string, msg tipos.Mensagem) error {
	if endereco == "" {
		return fmt.Errorf("endereço vazio")
	}

	enderecoUDP, err := net.ResolveUDPAddr("udp", endereco)
	if err != nil {
		return fmt.Errorf("falha ao resolver endereço UDP %s: %v", endereco, err)
	}

	conexao, err := net.DialUDP("udp", nil, enderecoUDP)
	if err != nil {
		return fmt.Errorf("falha ao conectar UDP em %s: %v", endereco, err)
	}
	defer conexao.Close()

	dados, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("falha ao serializar mensagem: %v", err)
	}

	_, err = conexao.Write(dados)
	if err != nil {
		return fmt.Errorf("falha ao enviar dados UDP: %v", err)
	}

	return nil
}

// Parar interrompe o protocolo gossip
// CORRIGIDO: Melhorado processo de parada com sincronização
func (pg *ProtocoloGossip) Parar() {
	pg.executando.Store(false)

	// Cancela contexto
	pg.cancel()

	// Aguarda um pouco para goroutines terminarem
	time.Sleep(100 * time.Millisecond)

	// Fecha canal de atualização
	close(pg.canalAtualizacao)

	utils.RegistrarLog("INFO", "Protocolo gossip parado para corretor %s", pg.idCorretor)
}
