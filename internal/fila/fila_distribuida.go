package fila

import (
	"container/heap"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"sync"
	"time"
)

// ItemFila representa um item na fila de prioridade
type ItemFila struct {
	Requisicao   *tipos.Requisicao
	Prioridade   int
	Indice       int
	CarimboTempo time.Time
}

// FilaPrioridade implementa heap.Interface para fila de prioridade
type FilaPrioridade []*ItemFila

func (fp FilaPrioridade) Len() int { return len(fp) }

func (fp FilaPrioridade) Less(i, j int) bool {
	// Primeiro por prioridade, depois por timestamp
	if fp[i].Prioridade != fp[j].Prioridade {
		return fp[i].Prioridade > fp[j].Prioridade
	}
	return fp[i].CarimboTempo.Before(fp[j].CarimboTempo)
}

func (fp FilaPrioridade) Swap(i, j int) {
	fp[i], fp[j] = fp[j], fp[i]
	fp[i].Indice = i
	fp[j].Indice = j
}

func (fp *FilaPrioridade) Push(x interface{}) {
	n := len(*fp)
	item := x.(*ItemFila)
	item.Indice = n
	*fp = append(*fp, item)
}

func (fp *FilaPrioridade) Pop() interface{} {
	old := *fp
	n := len(old)
	item := old[n-1]
	item.Indice = -1
	*fp = old[0 : n-1]
	return item
}

// FilaDistribuida gerencia uma fila distribuída de requisições
type FilaDistribuida struct {
	idCorretor         string
	fila               FilaPrioridade
	requisicoesPorID   map[string]*ItemFila
	mutex              sync.RWMutex
	canalProcessamento chan *tipos.Requisicao
	executando         bool
}

// NovaFilaDistribuida cria uma nova fila distribuída
func NovaFilaDistribuida(idCorretor string) *FilaDistribuida {
	fd := &FilaDistribuida{
		idCorretor:         idCorretor,
		fila:               make(FilaPrioridade, 0),
		requisicoesPorID:   make(map[string]*ItemFila),
		canalProcessamento: make(chan *tipos.Requisicao, 100),
		executando:         true,
	}

	heap.Init(&fd.fila)
	return fd
}

// AdicionarRequisicao adiciona uma requisição à fila
func (fd *FilaDistribuida) AdicionarRequisicao(req *tipos.Requisicao) error {
	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	if _, existe := fd.requisicoesPorID[req.ID]; existe {
		return nil // Requisição já existe
	}

	item := &ItemFila{
		Requisicao:   req,
		Prioridade:   req.Prioridade,
		CarimboTempo: req.CarimboTempo,
	}

	heap.Push(&fd.fila, item)
	fd.requisicoesPorID[req.ID] = item

	utils.RegistrarLog("INFO", "Requisição %s adicionada à fila do corretor %s", req.ID, fd.idCorretor)

	// Notifica processamento
	go fd.notificarProximaRequisicao()

	return nil
}

// RemoverRequisicao remove uma requisição da fila
func (fd *FilaDistribuida) RemoverRequisicao(idRequisicao string) bool {
	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	if item, existe := fd.requisicoesPorID[idRequisicao]; existe {
		heap.Remove(&fd.fila, item.Indice)
		delete(fd.requisicoesPorID, idRequisicao)
		return true
	}

	return false
}

// ObterProximaRequisicao obtém a próxima requisição da fila
func (fd *FilaDistribuida) ObterProximaRequisicao() *tipos.Requisicao {
	fd.mutex.Lock()
	defer fd.mutex.Unlock()

	if fd.fila.Len() == 0 {
		return nil
	}

	item := heap.Pop(&fd.fila).(*ItemFila)
	delete(fd.requisicoesPorID, item.Requisicao.ID)

	return item.Requisicao
}

// notificarProximaRequisicao notifica o canal de processamento
func (fd *FilaDistribuida) notificarProximaRequisicao() {
	fd.mutex.RLock()
	defer fd.mutex.RUnlock()

	if fd.fila.Len() > 0 {
		req := fd.fila[0].Requisicao
		select {
		case fd.canalProcessamento <- req:
		default:
		}
	}
}

// ObterCanalProcessamento retorna o canal de processamento
func (fd *FilaDistribuida) ObterCanalProcessamento() <-chan *tipos.Requisicao {
	return fd.canalProcessamento
}

// Tamanho retorna o tamanho da fila
func (fd *FilaDistribuida) Tamanho() int {
	fd.mutex.RLock()
	defer fd.mutex.RUnlock()
	return fd.fila.Len()
}

// Parar interrompe a fila distribuída
func (fd *FilaDistribuida) Parar() {
	fd.executando = false
	close(fd.canalProcessamento)
}
