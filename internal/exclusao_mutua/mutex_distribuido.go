package exclusao_mutua

import (
	"encoding/json"
	"net"
	"sistema-distribuido-brokers/pkg/tipos"
	"sistema-distribuido-brokers/pkg/utils"
	"sync"
	"time"
)

// MutexDistribuido implementa exclusão mútua distribuída
type MutexDistribuido struct {
	idCorretor       string
	vizinhos         map[string]*tipos.Vizinho
	recursoBloqueado map[string]bool
	filaEspera       map[string][]string
	mutex            sync.RWMutex
	tempoEsperaLock  time.Duration
}

// NovoMutexDistribuido cria um novo mutex distribuído
func NovoMutexDistribuido(idCorretor string, vizinhos map[string]*tipos.Vizinho) *MutexDistribuido {
	return &MutexDistribuido{
		idCorretor:       idCorretor,
		vizinhos:         vizinhos,
		recursoBloqueado: make(map[string]bool),
		filaEspera:       make(map[string][]string),
		tempoEsperaLock:  10 * time.Second,
	}
}

// SolicitarAcesso solicita acesso exclusivo a um recurso
func (md *MutexDistribuido) SolicitarAcesso(recursoID string) (bool, error) {
	md.mutex.Lock()

	// Verifica se o recurso já está bloqueado
	if bloqueado, existe := md.recursoBloqueado[recursoID]; existe && bloqueado {
		md.mutex.Unlock()
		return false, nil
	}

	// Marca como bloqueado temporariamente
	md.recursoBloqueado[recursoID] = true
	md.mutex.Unlock()

	// Solicita permissão dos vizinhos
	aprovacoes := md.solicitarPermissoes(recursoID)

	// Aguarda aprovações (maioria simples)
	maioria := (len(md.vizinhos) / 2) + 1

	select {
	case <-aprovacoes:
		if md.contarAprovacoes(recursoID) >= maioria {
			utils.RegistrarLog("INFO", "Corretor %s obteve lock para recurso %s",
				md.idCorretor, recursoID)
			return true, nil
		}
	case <-time.After(md.tempoEsperaLock):
		utils.RegistrarLog("AVISO", "Timeout ao solicitar lock para recurso %s", recursoID)
	}

	// Libera o recurso em caso de falha
	md.LiberarAcesso(recursoID)
	return false, nil
}

// solicitarPermissoes envia solicitações de permissão para vizinhos
func (md *MutexDistribuido) solicitarPermissoes(recursoID string) <-chan struct{} {
	aprovacoes := make(chan struct{}, len(md.vizinhos))

	mensagem := tipos.Mensagem{
		Tipo:         "SOLICITACAO_LOCK",
		OrigemID:     md.idCorretor,
		Dados:        map[string]string{"recurso_id": recursoID},
		CarimboTempo: time.Now(),
	}

	for _, vizinho := range md.vizinhos {
		if !vizinho.Ativo {
			continue
		}

		go func(v *tipos.Vizinho) {
			if md.enviarSolicitacaoTCP(v.EnderecoTCP, mensagem) {
				aprovacoes <- struct{}{}
			}
		}(vizinho)
	}

	return aprovacoes
}

// enviarSolicitacaoTCP envia solicitação via TCP
func (md *MutexDistribuido) enviarSolicitacaoTCP(endereco string, msg tipos.Mensagem) bool {
	conexao, err := net.DialTimeout("tcp", endereco, 5*time.Second)
	if err != nil {
		return false
	}
	defer conexao.Close()

	dados, err := json.Marshal(msg)
	if err != nil {
		return false
	}

	_, err = conexao.Write(append(dados, '\n'))
	return err == nil
}

// ProcessarSolicitacaoLock processa uma solicitação de lock
func (md *MutexDistribuido) ProcessarSolicitacaoLock(msg tipos.Mensagem) bool {
	dados, ok := msg.Dados.(map[string]interface{})
	if !ok {
		return false
	}

	recursoID, existe := dados["recurso_id"].(string)
	if !existe {
		return false
	}

	md.mutex.Lock()
	defer md.mutex.Unlock()

	// Verifica se o recurso está livre
	if bloqueado, existe := md.recursoBloqueado[recursoID]; existe && bloqueado {
		// Adiciona à fila de espera
		md.filaEspera[recursoID] = append(md.filaEspera[recursoID], msg.OrigemID)
		return false
	}

	return true
}

// LiberarAcesso libera o acesso a um recurso
func (md *MutexDistribuido) LiberarAcesso(recursoID string) {
	md.mutex.Lock()
	defer md.mutex.Unlock()

	delete(md.recursoBloqueado, recursoID)

	// Notifica próximo da fila de espera
	if fila, existe := md.filaEspera[recursoID]; existe && len(fila) > 0 {
		proximo := fila[0]
		md.filaEspera[recursoID] = fila[1:]

		// Notifica o próximo corretor
		if vizinho, existe := md.vizinhos[proximo]; existe {
			go md.notificarLiberacao(vizinho.EnderecoTCP, recursoID)
		}
	}

	utils.RegistrarLog("INFO", "Corretor %s liberou lock do recurso %s", md.idCorretor, recursoID)
}

// notificarLiberacao notifica um corretor sobre liberação de recurso
func (md *MutexDistribuido) notificarLiberacao(endereco, recursoID string) {
	mensagem := tipos.Mensagem{
		Tipo:         "LIBERACAO_LOCK",
		OrigemID:     md.idCorretor,
		Dados:        map[string]string{"recurso_id": recursoID},
		CarimboTempo: time.Now(),
	}

	md.enviarSolicitacaoTCP(endereco, mensagem)
}

// contarAprovacoes conta aprovações para um recurso
func (md *MutexDistribuido) contarAprovacoes(recursoID string) int {
	// Implementação simplificada - em produção seria necessário rastrear aprovações
	return len(md.vizinhos) / 2
}
