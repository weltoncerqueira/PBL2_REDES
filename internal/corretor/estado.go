package corretor

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"sync"
	"time"
)

// GerenciadorEstado gerencia o estado do corretor
type GerenciadorEstado struct {
	estado        *tipos.EstadoCorretor
	arquivoEstado string
	mutex         sync.RWMutex
}

// NovoGerenciadorEstado cria um novo gerenciador de estado
func NovoGerenciadorEstado(idCorretor string) *GerenciadorEstado {
	return &GerenciadorEstado{
		estado: &tipos.EstadoCorretor{
			ID:                idCorretor,
			LiderAtual:        "",
			Vizinhos:          make(map[string]tipos.Vizinho),
			Recursos:          make(map[string]tipos.Recurso),
			UltimaAtualizacao: time.Now(),
			Versao:            1,
		},
		arquivoEstado: "/tmp/corretor_estado_" + idCorretor + ".json",
	}
}

// AtualizarVizinho atualiza informações de um vizinho
func (ge *GerenciadorEstado) AtualizarVizinho(id string, enderecoTCP, enderecoUDP string) {
	ge.mutex.Lock()
	defer ge.mutex.Unlock()

	vizinho := tipos.Vizinho{
		ID:              id,
		EnderecoTCP:     enderecoTCP,
		EnderecoUDP:     enderecoUDP,
		UltimoBatimento: time.Now(),
		Ativo:           true,
		VersaoEstado:    1,
	}

	ge.estado.Vizinhos[id] = vizinho
	ge.estado.Versao++
	ge.estado.UltimaAtualizacao = time.Now()
}

// AtualizarRecurso atualiza informações de um recurso
func (ge *GerenciadorEstado) AtualizarRecurso(recurso tipos.Recurso) {
	ge.mutex.Lock()
	defer ge.mutex.Unlock()

	recurso.Versao++
	ge.estado.Recursos[recurso.ID] = recurso
	ge.estado.Versao++
	ge.estado.UltimaAtualizacao = time.Now()
}

// AtualizarLider atualiza o líder atual
func (ge *GerenciadorEstado) AtualizarLider(liderID string) {
	ge.mutex.Lock()
	defer ge.mutex.Unlock()

	ge.estado.LiderAtual = liderID
	ge.estado.Versao++
	ge.estado.UltimaAtualizacao = time.Now()
}

// ObterEstado retorna uma cópia do estado atual
func (ge *GerenciadorEstado) ObterEstado() *tipos.EstadoCorretor {
	ge.mutex.RLock()
	defer ge.mutex.RUnlock()

	// Cria uma cópia profunda do estado
	copia := &tipos.EstadoCorretor{
		ID:                ge.estado.ID,
		LiderAtual:        ge.estado.LiderAtual,
		Vizinhos:          make(map[string]tipos.Vizinho),
		Recursos:          make(map[string]tipos.Recurso),
		UltimaAtualizacao: ge.estado.UltimaAtualizacao,
		Versao:            ge.estado.Versao,
	}

	for k, v := range ge.estado.Vizinhos {
		copia.Vizinhos[k] = v
	}

	for k, v := range ge.estado.Recursos {
		copia.Recursos[k] = v
	}

	return copia
}

// ObterVizinhosAtivos retorna lista de vizinhos ativos
func (ge *GerenciadorEstado) ObterVizinhosAtivos() map[string]*tipos.Vizinho {
	ge.mutex.RLock()
	defer ge.mutex.RUnlock()

	ativos := make(map[string]*tipos.Vizinho)

	for id, vizinho := range ge.estado.Vizinhos {
		if vizinho.Ativo {
			v := vizinho
			ativos[id] = &v
		}
	}

	return ativos
}

// MarcarVizinhoInativo marca um vizinho como inativo
func (ge *GerenciadorEstado) MarcarVizinhoInativo(id string) {
	ge.mutex.Lock()
	defer ge.mutex.Unlock()

	if vizinho, existe := ge.estado.Vizinhos[id]; existe {
		vizinho.Ativo = false
		ge.estado.Vizinhos[id] = vizinho
		ge.estado.Versao++
		ge.estado.UltimaAtualizacao = time.Now()
	}
}

// SalvarEstado persiste o estado em disco
func (ge *GerenciadorEstado) SalvarEstado() error {
	ge.mutex.RLock()
	defer ge.mutex.RUnlock()

	dados, err := json.MarshalIndent(ge.estado, "", "  ")
	if err != nil {
		return err
	}

	return ioutil.WriteFile(ge.arquivoEstado, dados, 0644)
}

// CarregarEstado carrega o estado do disco
func (ge *GerenciadorEstado) CarregarEstado() error {
	dados, err := ioutil.ReadFile(ge.arquivoEstado)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var estado tipos.EstadoCorretor
	if err := json.Unmarshal(dados, &estado); err != nil {
		return err
	}

	ge.mutex.Lock()
	defer ge.mutex.Unlock()

	ge.estado = &estado
	utils.RegistrarLog("INFO", "Estado carregado do disco para corretor %s", ge.estado.ID)

	return nil
}
