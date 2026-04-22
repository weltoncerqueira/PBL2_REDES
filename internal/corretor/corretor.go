package corretor

import (
	"context"
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
	ctx                   context.Context    // NOVO: contexto para controlar goroutines
	cancel                context.CancelFunc // NOVO: função para cancelar contexto
}

// NovoCorretor cria uma nova instância de Corretor
func NovoCorretor(id, portaTCP, portaUDP string, listaVizinhos []string) (*Corretor, error) {
	estado := NovoGerenciadorEstado(id)

	// Carrega estado persistido se existir
	if err := estado.CarregarEstado(); err != nil {
		utils.RegistrarLog("AVISO", "Não foi possível carregar estado anterior: %v", err)
	}

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

	// Inicializa contexto para cancelamento
	c.ctx, c.cancel = context.WithCancel(context.Background())

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
// CORRIGIDO: Adicionado defer para recovery e verificação de contexto
func (c *Corretor) processarMensagensTCP() {
	defer func() {
		if r := recover(); r != nil {
			utils.RegistrarLog("ERRO", "Panic em processarMensagensTCP: %v", r)
		}
		if c.listenerTCP != nil {
			c.listenerTCP.Close()
		}
	}()

	for c.executando {
		select {
		case <-c.ctx.Done():
			utils.RegistrarLog("INFO", "processarMensagensTCP cancelado")
			return
		default:
		}

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
// CORRIGIDO: Adicionado defer com recovery e timeout
func (c *Corretor) tratarConexaoTCP(conexao net.Conn) {
	defer func() {
		if r := recover(); r != nil {
			utils.RegistrarLog("ERRO", "Panic em tratarConexaoTCP: %v", r)
		}
		conexao.Close()
	}()

	// Define timeout para a conexão
	conexao.SetReadDeadline(time.Now().Add(30 * time.Second))

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
// CORRIGIDO: Melhorado tratamento de erro ao enviar resposta
func (c *Corretor) tratarSolicitacaoLock(mensagem tipos.Mensagem) {
	aprovado := c.mutexDistribuido.ProcessarSolicitacaoLock(mensagem)

	resposta := tipos.Mensagem{
		Tipo:         "RESPOSTA_LOCK",
		OrigemID:     c.id,
		DestinoID:    mensagem.OrigemID,
		Dados:        map[string]bool{"aprovado": aprovado},
		CarimboTempo: time.Now(),
	}

	vizinhosAtivos := c.estado.ObterVizinhosAtivos()
	if vizinho, existe := vizinhosAtivos[mensagem.OrigemID]; existe {
		if err := c.enviarRespostaTCP(vizinho.EnderecoTCP, resposta); err != nil {
			utils.RegistrarLog("AVISO", "Falha ao enviar resposta de lock para %s: %v",
				mensagem.OrigemID, err)
		}
	}
}

// tratarRequisicao processa uma requisição recebida
// CORRIGIDO: Conversão segura de tipos com validação
func (c *Corretor) tratarRequisicao(mensagem tipos.Mensagem) {
	if dados, ok := mensagem.Dados.(map[string]interface{}); ok {
		// Extração segura de valores do mapa
		tipo, tipoOk := dados["tipo"].(string)
		recursoID, recursoOk := dados["recurso_id"].(string)
		prioridadeRaw, prioridadeOk := dados["prioridade"]

		if !tipoOk || !recursoOk {
			utils.RegistrarLog("ERRO", "Requisição com dados incompletos de %s", mensagem.OrigemID)
			return
		}

		// Extração segura de prioridade
		prioridade := 0
		if prioridadeOk {
			if floatVal, ok := prioridadeRaw.(float64); ok {
				prioridade = int(floatVal)
			}
		}

		requisicao := &tipos.Requisicao{
			ID:             fmt.Sprintf("req-%d", time.Now().UnixNano()),
			Tipo:           tipo,
			CorretorOrigem: mensagem.OrigemID,
			RecursoID:      recursoID,
			Estado:         "pendente",
			CarimboTempo:   time.Now(),
			Prioridade:     prioridade,
		}

		if err := c.filaDistribuida.AdicionarRequisicao(requisicao); err != nil {
			utils.RegistrarLog("ERRO", "Falha ao adicionar requisição à fila: %v", err)
		}
	}
}

// enviarRespostaTCP envia uma resposta via TCP
// CORRIGIDO: Melhorado tratamento de erros e logging
func (c *Corretor) enviarRespostaTCP(endereco string, resposta tipos.Mensagem) error {
	conexao, err := net.DialTimeout("tcp", endereco, 5*time.Second)
	if err != nil {
		return fmt.Errorf("falha ao conectar em %s: %v", endereco, err)
	}
	defer conexao.Close()

	dados, err := json.Marshal(resposta)
	if err != nil {
		return fmt.Errorf("falha ao serializar resposta: %v", err)
	}

	_, err = conexao.Write(append(dados, '\n'))
	if err != nil {
		return fmt.Errorf("falha ao enviar resposta: %v", err)
	}

	return nil
}

// processarRequisicoes processa requisições da fila
// CORRIGIDO: Captura líder uma vez para evitar race condition
func (c *Corretor) processarRequisicoes() {
	defer func() {
		if r := recover(); r != nil {
			utils.RegistrarLog("ERRO", "Panic em processarRequisicoes: %v", r)
		}
	}()

	for {
		select {
		case <-c.ctx.Done():
			utils.RegistrarLog("INFO", "processarRequisicoes cancelado")
			return
		case requisicao, aberto := <-c.filaDistribuida.ObterCanalProcessamento():
			if !aberto {
				utils.RegistrarLog("INFO", "Canal de processamento fechado")
				return
			}

			if requisicao == nil {
				continue
			}

			// Captura líder uma vez
			liderAtual := c.algoritmoEleicao.ObterLiderAtual()

			if liderAtual != c.id {
				// Encaminha para o líder se não for o próprio
				c.encaminharRequisicaoParaLider(requisicao)
				continue
			}

			// Processa a requisição
			c.executarRequisicao(requisicao)
		}
	}
}

// executarRequisicao executa uma requisição
// CORRIGIDO: Melhorado tratamento de erros
// executarRequisicao executa uma requisição
// CORRIGIDO: Melhorado tratamento de erros
func (c *Corretor) executarRequisicao(req *tipos.Requisicao) {
	utils.RegistrarLog("INFO", "Processando requisição %s do tipo %s", req.ID, req.Tipo)

	switch req.Tipo {
	case "ALOCAR_RECURSO":
		// Solicita lock distribuído
		aprovado, err := c.mutexDistribuido.SolicitarAcesso(req.RecursoID)
		if err != nil {
			utils.RegistrarLog("ERRO", "Falha ao solicitar lock para %s: %v", req.RecursoID, err)
			req.Estado = "erro"
			break
		}

		if aprovado {
			recurso, err := c.gerenciadorRecursos.AlocarRecurso(req.RecursoID)
			if err != nil {
				utils.RegistrarLog("ERRO", "Falha ao alocar recurso %s: %v", req.RecursoID, err)
				req.Estado = "erro"
				c.mutexDistribuido.LiberarAcesso(req.RecursoID)
				break
			}

			if recurso != nil {
				req.Estado = "concluido"
				utils.RegistrarLog("INFO", "Requisição %s concluída com sucesso", req.ID)
			} else {
				req.Estado = "erro"
				utils.RegistrarLog("AVISO", "Recurso %s não disponível", req.RecursoID)
			}

			// CORRIGIDO: Sem atribuição de erro
			c.mutexDistribuido.LiberarAcesso(req.RecursoID)
		} else {
			req.Estado = "negado"
			utils.RegistrarLog("AVISO", "Lock negado para recurso %s", req.RecursoID)
		}

	case "LIBERAR_RECURSO":
		if err := c.gerenciadorRecursos.LiberarRecurso(req.RecursoID); err != nil {
			utils.RegistrarLog("ERRO", "Falha ao liberar recurso %s: %v", req.RecursoID, err)
			req.Estado = "erro"
			break
		}
		req.Estado = "concluido"

	case "CONSULTAR_RECURSOS":
		recursos := c.gerenciadorRecursos.ObterRecursosDisponiveis()
		req.Dados = recursos
		req.Estado = "concluido"

	default:
		utils.RegistrarLog("AVISO", "Tipo de requisição desconhecido: %s", req.Tipo)
		req.Estado = "erro"
	}

	// Salva estado periodicamente
	if err := c.estado.SalvarEstado(); err != nil {
		utils.RegistrarLog("ERRO", "Falha ao salvar estado: %v", err)
	}
}

// encaminharRequisicaoParaLider encaminha requisição para o líder
// CORRIGIDO: Melhorado tratamento de erro
func (c *Corretor) encaminharRequisicaoParaLider(req *tipos.Requisicao) {
	liderID := c.algoritmoEleicao.ObterLiderAtual()
	if liderID == "" || liderID == c.id {
		utils.RegistrarLog("AVISO", "Líder inválido ou vazio: %s", liderID)
		return
	}

	vizinhosAtivos := c.estado.ObterVizinhosAtivos()
	lider, existe := vizinhosAtivos[liderID]
	if !existe {
		utils.RegistrarLog("AVISO", "Líder %s não encontrado na lista de vizinhos", liderID)
		return
	}

	mensagem := tipos.Mensagem{
		Tipo:         "REQUISICAO",
		OrigemID:     c.id,
		DestinoID:    liderID,
		Dados:        req,
		CarimboTempo: time.Now(),
	}

	if err := c.enviarRespostaTCP(lider.EnderecoTCP, mensagem); err != nil {
		utils.RegistrarLog("AVISO", "Falha ao encaminhar requisição para líder %s: %v", liderID, err)
	}
}

// monitorarEleicao monitora mudanças de liderança
// CORRIGIDO: Adicionado verificação de contexto
func (c *Corretor) monitorarEleicao() {
	defer func() {
		if r := recover(); r != nil {
			utils.RegistrarLog("ERRO", "Panic em monitorarEleicao: %v", r)
		}
	}()

	canalResultado := c.algoritmoEleicao.ObterCanalResultado()
	for {
		select {
		case <-c.ctx.Done():
			utils.RegistrarLog("INFO", "monitorarEleicao cancelado")
			return
		case resultado, aberto := <-canalResultado:
			if !aberto {
				return
			}

			c.estado.AtualizarLider(resultado)
			utils.RegistrarLog("INFO", "Novo líder eleito: %s", resultado)
		}
	}
}

// monitorarFalhas monitora falhas de vizinhos
// CORRIGIDO: Adicionado verificação de contexto
func (c *Corretor) monitorarFalhas() {
	defer func() {
		if r := recover(); r != nil {
			utils.RegistrarLog("ERRO", "Panic em monitorarFalhas: %v", r)
		}
	}()

	canalFalha := c.gerenciadorBatimentos.ObterCanalFalha()
	for {
		select {
		case <-c.ctx.Done():
			utils.RegistrarLog("INFO", "monitorarFalhas cancelado")
			return
		case vizinhoID, aberto := <-canalFalha:
			if !aberto {
				return
			}

			c.estado.MarcarVizinhoInativo(vizinhoID)

			// Se o líder falhou, inicia nova eleição
			liderAtual := c.algoritmoEleicao.ObterLiderAtual()
			if liderAtual == vizinhoID {
				utils.RegistrarLog("AVISO", "Líder %s falhou, iniciando nova eleição", vizinhoID)
				go c.algoritmoEleicao.IniciarEleicao()
			}
		}
	}
}

// Parar interrompe o corretor
// CORRIGIDO: Melhorado processo de parada com sincronização
func (c *Corretor) Parar() {
	c.mutex.Lock()
	if !c.executando {
		c.mutex.Unlock()
		return
	}
	c.executando = false
	c.mutex.Unlock()

	// Cancela todos os contextos
	c.cancel()

	// Aguarda um pouco para as goroutines terminarem
	time.Sleep(100 * time.Millisecond)

	// Fecha listener TCP
	if c.listenerTCP != nil {
		if err := c.listenerTCP.Close(); err != nil {
			utils.RegistrarLog("AVISO", "Erro ao fechar listener TCP: %v", err)
		}
	}

	// Para componentes
	c.gerenciadorBatimentos.Parar()
	c.protocoloGossip.Parar()
	c.filaDistribuida.Parar()

	// Salva estado final
	if err := c.estado.SalvarEstado(); err != nil {
		utils.RegistrarLog("ERRO", "Falha ao salvar estado final: %v", err)
	}

	// Fecha canal de controle
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
