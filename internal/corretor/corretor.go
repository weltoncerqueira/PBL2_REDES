package corretor

import (
	"encoding/json"
	"fmt"
	"net"
	"sistema-distribuido-brokers/internal/eleicao"
	"sistema-distribuido-brokers/internal/exclusao_mutua"
	"sistema-distribuido-brokers/internal/fila"
	"sistema-distribuido-brokers/internal/gossip"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"strings"
	"sync"
	"time"
)

// Corretor representa um broker no sistema distribuído
type Corretor struct {
	id                    string
	portaTCP              string
	portaUDP              string
	estado                *GerenciadorEstado
	gerenciadorRecursos   *GerenciadorRecursos
	algoritmoEleicao      *eleicao.AlgoritmoBully
	gerenciadorBatimentos *gossip.GerenciadorBatimentos
	protocoloGossip       *gossip.ProtocoloGossip
	filaDistribuida       *fila.FilaDistribuida
	mutexDistribuido      *exclusao_mutua.MutexDistribuido
	listenerTCP           net.Listener
	listenerUDP           *net.UDPConn
	executando            bool
	mutex                 sync.RWMutex
	canalControle         chan struct{}
}

// NovoCorretor cria uma nova instância de Corretor
func NovoCorretor(id, portaTCP, portaUDP string, listaVizinhos []string) (*Corretor, error) {
	estado := NovoGerenciadorEstado(id)

	// Carrega estado persistido se existir
	estado.CarregarEstado()

	c := &Corretor{
		id:                  id,
		portaTCP:            portaTCP,
		portaUDP:            portaUDP,
		estado:              estado,
		gerenciadorRecursos: NovoGerenciadorRecursos(id, estado),
		filaDistribuida:     fila.NovaFilaDistribuida(id),
		executando:          true,
		canalControle:       make(chan struct{}),
	}

	// Inicializa vizinhos
	for _, vizinho := range listaVizinhos {
		partes := strings.Split(vizinho, ",")
		if len(partes) == 3 {
			c.estado.AtualizarVizinho(partes[0], partes[1], partes[2])
		}
	}

	// Inicializa componentes
	vizinhos := c.estado.ObterVizinhosAtivos()

	var err error
	c.algoritmoEleicao = eleicao.NovaEleicaoBully(id, vizinhos)
	c.mutexDistribuido = exclusao_mutua.NovoMutexDistribuido(id, vizinhos)

	c.gerenciadorBatimentos, err = gossip.NovoGerenciadorBatimentos(id, vizinhos, portaUDP)
	if err != nil {
		return nil, fmt.Errorf("falha ao criar gerenciador de batimentos: %v", err)
	}

	c.protocoloGossip = gossip.NovoProtocoloGossip(id, estado.ObterEstado(), vizinhos)

	return c, nil
}

// Iniciar inicia todos os serviços do corretor
func (c *Corretor) Iniciar() error {
	// Inicia listeners
	if err := c.iniciarListenerTCP(); err != nil {
		return fmt.Errorf("falha ao iniciar listener TCP: %v", err)
	}

	// Inicia componentes
	c.gerenciadorBatimentos.Iniciar()
	c.protocoloGossip.Iniciar()

	// Inicia goroutines de processamento
	go c.processarMensagensTCP()
	go c.processarRequisicoes()
	go c.monitorarEleicao()
	go c.monitorarFalhas()

	// Inicia eleição
	time.Sleep(2 * time.Second)
	go c.algoritmoEleicao.IniciarEleicao()

	utils.RegistrarLog("INFO", "Corretor %s iniciado com sucesso na porta TCP %s e UDP %s",
		c.id, c.portaTCP, c.portaUDP)

	return nil
}

// iniciarListenerTCP inicia o listener TCP
func (c *Corretor) iniciarListenerTCP() error {
	listener, err := net.Listen("tcp", c.portaTCP)
	if err != nil {
		return err
	}

	c.listenerTCP = listener
	return nil
}

// processarMensagensTCP processa mensagens recebidas via TCP
func (c *Corretor) processarMensagensTCP() {
	for c.executando {
		conexao, err := c.listenerTCP.Accept()
		if err != nil {
			if c.executando {
				utils.RegistrarLog("ERRO", "Erro ao aceitar conexão TCP: %v", err)
			}
			continue
		}

		go c.tratarConexaoTCP(conexao)
	}
}

// tratarConexaoTCP trata uma conexão TCP individual
func (c *Corretor) tratarConexaoTCP(conexao net.Conn) {
	defer conexao.Close()

	decoder := json.NewDecoder(conexao)
	var mensagem tipos.Mensagem

	if err := decoder.Decode(&mensagem); err != nil {
		utils.RegistrarLog("ERRO", "Erro ao decodificar mensagem TCP: %v", err)
		return
	}

	// Processa mensagem baseado no tipo
	switch mensagem.Tipo {
	case "ELEICAO", "RESPOSTA_ELEICAO", "VITORIA":
		c.algoritmoEleicao.ProcessarMensagemEleicao(mensagem)

	case "SOLICITACAO_LOCK", "LIBERACAO_LOCK":
		c.tratarSolicitacaoLock(mensagem)

	case "REQUISICAO":
		c.tratarRequisicao(mensagem)

	default:
		utils.RegistrarLog("AVISO", "Tipo de mensagem desconhecido: %s", mensagem.Tipo)
	}
}

// tratarSolicitacaoLock trata solicitações de lock
func (c *Corretor) tratarSolicitacaoLock(mensagem tipos.Mensagem) {
	aprovado := c.mutexDistribuido.ProcessarSolicitacaoLock(mensagem)

	resposta := tipos.Mensagem{
		Tipo:         "RESPOSTA_LOCK",
		OrigemID:     c.id,
		DestinoID:    mensagem.OrigemID,
		Dados:        map[string]bool{"aprovado": aprovado},
		CarimboTempo: time.Now(),
	}

	if vizinho, existe := c.estado.ObterVizinhosAtivos()[mensagem.OrigemID]; existe {
		c.enviarRespostaTCP(vizinho.EnderecoTCP, resposta)
	}
}

// tratarRequisicao processa uma requisição recebida
func (c *Corretor) tratarRequisicao(mensagem tipos.Mensagem) {
	if dados, ok := mensagem.Dados.(map[string]interface{}); ok {
		requisicao := &tipos.Requisicao{
			ID:             fmt.Sprintf("req-%d", time.Now().UnixNano()),
			Tipo:           dados["tipo"].(string),
			CorretorOrigem: mensagem.OrigemID,
			RecursoID:      dados["recurso_id"].(string),
			Estado:         "pendente",
			CarimboTempo:   time.Now(),
			Prioridade:     int(dados["prioridade"].(float64)),
		}

		c.filaDistribuida.AdicionarRequisicao(requisicao)
	}
}

// enviarRespostaTCP envia uma resposta via TCP
func (c *Corretor) enviarRespostaTCP(endereco string, resposta tipos.Mensagem) error {
	conexao, err := net.DialTimeout("tcp", endereco, 5*time.Second)
	if err != nil {
		return err
	}
	defer conexao.Close()

	dados, err := json.Marshal(resposta)
	if err != nil {
		return err
	}

	_, err = conexao.Write(append(dados, '\n'))
	return err
}

// processarRequisicoes processa requisições da fila
func (c *Corretor) processarRequisicoes() {
	for requisicao := range c.filaDistribuida.ObterCanalProcessamento() {
		// Verifica se é o líder
		if c.algoritmoEleicao.ObterLiderAtual() != c.id {
			// Encaminha para o líder
			c.encaminharRequisicaoParaLider(requisicao)
			continue
		}

		// Processa a requisição
		c.executarRequisicao(requisicao)
	}
}

// executarRequisicao executa uma requisição
func (c *Corretor) executarRequisicao(req *tipos.Requisicao) {
	utils.RegistrarLog("INFO", "Processando requisição %s do tipo %s", req.ID, req.Tipo)

	switch req.Tipo {
	case "ALOCAR_RECURSO":
		// Solicita lock distribuído
		aprovado, _ := c.mutexDistribuido.SolicitarAcesso(req.RecursoID)
		if aprovado {
			recurso, _ := c.gerenciadorRecursos.AlocarRecurso(req.RecursoID)
			if recurso != nil {
				req.Estado = "concluido"
				utils.RegistrarLog("INFO", "Requisição %s concluída com sucesso", req.ID)
			}
			c.mutexDistribuido.LiberarAcesso(req.RecursoID)
		}

	case "LIBERAR_RECURSO":
		c.gerenciadorRecursos.LiberarRecurso(req.RecursoID)
		req.Estado = "concluido"

	case "CONSULTAR_RECURSOS":
		recursos := c.gerenciadorRecursos.ObterRecursosDisponiveis()
		req.Dados = recursos
		req.Estado = "concluido"
	}

	// Salva estado periodicamente
	c.estado.SalvarEstado()
}

// encaminharRequisicaoParaLider encaminha requisição para o líder
func (c *Corretor) encaminharRequisicaoParaLider(req *tipos.Requisicao) {
	liderID := c.algoritmoEleicao.ObterLiderAtual()
	if liderID == "" || liderID == c.id {
		return
	}

	if lider, existe := c.estado.ObterVizinhosAtivos()[liderID]; existe {
		mensagem := tipos.Mensagem{
			Tipo:         "REQUISICAO",
			OrigemID:     c.id,
			DestinoID:    liderID,
			Dados:        req,
			CarimboTempo: time.Now(),
		}

		c.enviarRespostaTCP(lider.EnderecoTCP, mensagem)
	}
}

// monitorarEleicao monitora mudanças de liderança
func (c *Corretor) monitorarEleicao() {
	for resultado := range c.algoritmoEleicao.ObterCanalResultado() {
		c.estado.AtualizarLider(resultado)
		utils.RegistrarLog("INFO", "Novo líder eleito: %s", resultado)
	}
}

// monitorarFalhas monitora falhas de vizinhos
func (c *Corretor) monitorarFalhas() {
	for vizinhoID := range c.gerenciadorBatimentos.ObterCanalFalha() {
		c.estado.MarcarVizinhoInativo(vizinhoID)

		// Se o líder falhou, inicia nova eleição
		if c.algoritmoEleicao.ObterLiderAtual() == vizinhoID {
			utils.RegistrarLog("AVISO", "Líder %s falhou, iniciando nova eleição", vizinhoID)
			go c.algoritmoEleicao.IniciarEleicao()
		}
	}
}

// Parar interrompe o corretor
func (c *Corretor) Parar() {
	c.executando = false

	if c.listenerTCP != nil {
		c.listenerTCP.Close()
	}

	c.gerenciadorBatimentos.Parar()
	c.protocoloGossip.Parar()
	c.filaDistribuida.Parar()

	c.estado.SalvarEstado()

	close(c.canalControle)
	utils.RegistrarLog("INFO", "Corretor %s parado", c.id)
}

// ObterID retorna o ID do corretor
func (c *Corretor) ObterID() string {
	return c.id
}

// ObterLiderAtual retorna o líder atual
func (c *Corretor) ObterLiderAtual() string {
	return c.algoritmoEleicao.ObterLiderAtual()
}
