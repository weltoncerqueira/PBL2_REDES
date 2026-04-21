package tipos

import (
	"sync"
	"time"
)

// Mensagem representa uma mensagem trocada entre corretores
type Mensagem struct {
	Tipo            string      `json:"tipo"`
	OrigemID        string      `json:"origem_id"`
	DestinoID       string      `json:"destino_id"`
	Dados           interface{} `json:"dados"`
	CarimboTempo    time.Time   `json:"carimbo_tempo"`
	NumeroSequencia uint64      `json:"numero_sequencia"`
}

// EstadoCorretor representa o estado de um corretor
type EstadoCorretor struct {
	ID                string             `json:"id"`
	LiderAtual        string             `json:"lider_atual"`
	Vizinhos          map[string]Vizinho `json:"vizinhos"`
	Recursos          map[string]Recurso `json:"recursos"`
	UltimaAtualizacao time.Time          `json:"ultima_atualizacao"`
	Versao            uint64             `json:"versao"`
	sync.RWMutex
}

// Vizinho representa um corretor conhecido
type Vizinho struct {
	ID              string    `json:"id"`
	EnderecoTCP     string    `json:"endereco_tcp"`
	EnderecoUDP     string    `json:"endereco_udp"`
	UltimoBatimento time.Time `json:"ultimo_batimento"`
	Ativo           bool      `json:"ativo"`
	VersaoEstado    uint64    `json:"versao_estado"`
}

// Recurso representa um recurso gerenciado pelo sistema
type Recurso struct {
	ID            string    `json:"id"`
	Nome          string    `json:"nome"`
	Tipo          string    `json:"tipo"`
	Estado        string    `json:"estado"`
	CorretorAtual string    `json:"corretor_atual"`
	BloqueadoPor  string    `json:"bloqueado_por"`
	UltimoAcesso  time.Time `json:"ultimo_acesso"`
	Versao        uint64    `json:"versao"`
}

// Requisicao representa uma requisição no sistema
type Requisicao struct {
	ID             string      `json:"id"`
	Tipo           string      `json:"tipo"`
	CorretorOrigem string      `json:"corretor_origem"`
	RecursoID      string      `json:"recurso_id"`
	Dados          interface{} `json:"dados"`
	Estado         string      `json:"estado"`
	CarimboTempo   time.Time   `json:"carimbo_tempo"`
	Prioridade     int         `json:"prioridade"`
	Tentativas     int         `json:"tentativas"`
}

// Resposta representa uma resposta a uma requisição
type Resposta struct {
	RequisicaoID string      `json:"requisicao_id"`
	Sucesso      bool        `json:"sucesso"`
	Dados        interface{} `json:"dados"`
	Erro         string      `json:"erro,omitempty"`
	CarimboTempo time.Time   `json:"carimbo_tempo"`
}
