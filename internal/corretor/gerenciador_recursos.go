package corretor

import (
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"sync"
	"time"
)

// GerenciadorRecursos gerencia recursos do sistema
type GerenciadorRecursos struct {
	idCorretor string
	recursos   map[string]*tipos.Recurso
	estado     *GerenciadorEstado
	mutex      sync.RWMutex
}

// NovoGerenciadorRecursos cria um novo gerenciador de recursos
func NovoGerenciadorRecursos(idCorretor string, estado *GerenciadorEstado) *GerenciadorRecursos {
	gr := &GerenciadorRecursos{
		idCorretor: idCorretor,
		recursos:   make(map[string]*tipos.Recurso),
		estado:     estado,
	}

	// Inicializa alguns recursos de exemplo
	gr.inicializarRecursos()

	return gr
}

// inicializarRecursos inicializa recursos padrão
func (gr *GerenciadorRecursos) inicializarRecursos() {
	recursosIniciais := []tipos.Recurso{
		{
			ID:            "drone-001",
			Nome:          "Drone de Entrega 001",
			Tipo:          "drone",
			Estado:        "disponivel",
			CorretorAtual: "",
			BloqueadoPor:  "",
			UltimoAcesso:  time.Now(),
			Versao:        1,
		},
		{
			ID:            "drone-002",
			Nome:          "Drone de Entrega 002",
			Tipo:          "drone",
			Estado:        "disponivel",
			CorretorAtual: "",
			BloqueadoPor:  "",
			UltimoAcesso:  time.Now(),
			Versao:        1,
		},
		{
			ID:            "tarefa-001",
			Nome:          "Entrega Expressa",
			Tipo:          "tarefa",
			Estado:        "pendente",
			CorretorAtual: "",
			BloqueadoPor:  "",
			UltimoAcesso:  time.Now(),
			Versao:        1,
		},
	}

	for _, recurso := range recursosIniciais {
		r := recurso
		gr.recursos[recurso.ID] = &r
		gr.estado.AtualizarRecurso(recurso)
	}
}

// AlocarRecurso aloca um recurso para uso
func (gr *GerenciadorRecursos) AlocarRecurso(recursoID string) (*tipos.Recurso, error) {
	gr.mutex.Lock()
	defer gr.mutex.Unlock()

	recurso, existe := gr.recursos[recursoID]
	if !existe {
		return nil, nil
	}

	if recurso.Estado != "disponivel" {
		return nil, nil
	}

	recurso.Estado = "em_uso"
	recurso.CorretorAtual = gr.idCorretor
	recurso.BloqueadoPor = gr.idCorretor
	recurso.UltimoAcesso = time.Now()

	// Atualiza no estado global
	gr.estado.AtualizarRecurso(*recurso)

	utils.RegistrarLog("INFO", "Recurso %s alocado para corretor %s", recursoID, gr.idCorretor)

	return recurso, nil
}

// LiberarRecurso libera um recurso alocado
func (gr *GerenciadorRecursos) LiberarRecurso(recursoID string) error {
	gr.mutex.Lock()
	defer gr.mutex.Unlock()

	recurso, existe := gr.recursos[recursoID]
	if !existe {
		return nil
	}

	if recurso.BloqueadoPor != gr.idCorretor {
		return nil
	}

	recurso.Estado = "disponivel"
	recurso.BloqueadoPor = ""
	recurso.UltimoAcesso = time.Now()

	// Atualiza no estado global
	gr.estado.AtualizarRecurso(*recurso)

	utils.RegistrarLog("INFO", "Recurso %s liberado pelo corretor %s", recursoID, gr.idCorretor)

	return nil
}

// ObterRecursosDisponiveis retorna lista de recursos disponíveis
func (gr *GerenciadorRecursos) ObterRecursosDisponiveis() []*tipos.Recurso {
	gr.mutex.RLock()
	defer gr.mutex.RUnlock()

	var disponiveis []*tipos.Recurso

	for _, recurso := range gr.recursos {
		if recurso.Estado == "disponivel" {
			disponiveis = append(disponiveis, recurso)
		}
	}

	return disponiveis
}

// SincronizarRecursos sincroniza recursos do estado global
func (gr *GerenciadorRecursos) SincronizarRecursos(estado *tipos.EstadoCorretor) {
	gr.mutex.Lock()
	defer gr.mutex.Unlock()

	for id, recurso := range estado.Recursos {
		if recursoLocal, existe := gr.recursos[id]; existe {
			if recurso.Versao > recursoLocal.Versao {
				*recursoLocal = recurso
			}
		} else {
			r := recurso
			gr.recursos[id] = &r
		}
	}
}
