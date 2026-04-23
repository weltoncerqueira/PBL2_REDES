# 📋 Plano de Implementação de Correções

## Status: ✅ PRONTO PARA IMPLEMENTAR

---

## Fase 1: Correções Imediatas (Esta semana)

### 1.1 ✅ Arquivo: `internal/gossip/batimento.go`
**Versão corrigida disponível em:** `internal/gossip/batimento_corrigido.go`

**Mudanças principais:**
- ✅ Trocar `map[string]*tipos.Vizinho` por `sync.Map`
- ✅ Adicionar `sync.WaitGroup` para rastrear goroutines
- ✅ Adicionar `atomic.Bool` para `fechado`
- ✅ Implementar `sync.Map.Range()` para iteração segura
- ✅ Melhorar `Parar()` com WaitGroup timeout

**Passos:**
```bash
# 1. Backup da versão atual
cp internal/gossip/batimento.go internal/gossip/batimento_backup.go

# 2. Copiar versão corrigida
cp internal/gossip/batimento_corrigido.go internal/gossip/batimento.go

# 3. Testar
go test -race ./internal/gossip/...
```

---

### 1.2 ✅ Arquivo: `internal/corretor/corretor.go`

**Mudanças necessárias:**

#### A. Adicionar WaitGroup (linha ~20)
```go
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
    executando            atomic.Bool           // ✅ MUDOU: de bool para atomic.Bool
    wg                    sync.WaitGroup        // ✅ NOVO
    mutex                 sync.RWMutex
    canalControle         chan struct{}
    ctx                   context.Context
    cancel                context.CancelFunc
}
```

#### B. Refatorar `NovoCorretor` (linha ~41)
```go
func NovoCorretor(id, portaTCP, portaUDP string, listaVizinhos []string) (*Corretor, error) {
    estado := NovoGerenciadorEstado(id)
    
    if err := estado.CarregarEstado(); err != nil {
        utils.RegistrarLog("AVISO", "Não foi possível carregar estado anterior: %v", err)
    }
    
    // ✅ VALIDAR VIZINHOS PRIMEIRO
    vizinhosMap := make(map[string]*tipos.Vizinho)
    for _, vizinho := range listaVizinhos {
        partes := strings.Split(vizinho, ",")
        if len(partes) != 3 {
            return nil, fmt.Errorf("vizinho mal formatado: %s", vizinho)
        }
        
        vizinhosMap[partes[0]] = &tipos.Vizinho{
            ID:          partes[0],
            EnderecoTCP: partes[1],
            EnderecoUDP: partes[2],
            Ativo:       true,
        }
    }
    
    if len(vizinhosMap) == 0 {
        return nil, fmt.Errorf("nenhum vizinho configurado")
    }
    
    for id, vizinho := range vizinhosMap {
        estado.AtualizarVizinho(id, vizinho.EnderecoTCP, vizinho.EnderecoUDP)
    }
    
    c := &Corretor{
        id:                  id,
        portaTCP:            portaTCP,
        portaUDP:            portaUDP,
        estado:              estado,
        gerenciadorRecursos: NovoGerenciadorRecursos(id, estado),
        filaDistribuida:     fila.NovaFilaDistribuida(id),
        canalControle:       make(chan struct{}),
    }
    
    c.ctx, c.cancel = context.WithCancel(context.Background())
    c.executando.Store(true)
    
    vizinhosAtivos := c.estado.ObterVizinhosAtivos()
    c.algoritmoEleicao = eleicao.NovaEleicaoBully(id, vizinhosAtivos)
    // ... resto
    
    return c, nil
}
```

#### C. Refatorar `Iniciar` (linha ~89)
```go
func (c *Corretor) Iniciar() error {
    if err := c.iniciarListenerTCP(); err != nil {
        return fmt.Errorf("falha ao iniciar listener TCP: %v", err)
    }
    
    c.gerenciadorBatimentos.Iniciar()
    c.protocoloGossip.Iniciar()
    
    // ✅ Incrementar WaitGroup para cada goroutine
    c.wg.Add(4)
    
    go c.processarMensagensTCPComWait()
    go c.processarRequisicoesComWait()
    go c.monitorarEleicaoComWait()
    go c.monitorarFalhasComWait()
    
    time.Sleep(2 * time.Second)
    go c.algoritmoEleicao.IniciarEleicao()
    
    utils.RegistrarLog("INFO", "Corretor %s iniciado com sucesso", c.id)
    return nil
}

// ✅ NOVAS VERSÕES COM DEFER wg.Done()
func (c *Corretor) processarMensagensTCPComWait() {
    defer c.wg.Done()
    c.processarMensagensTCP()
}

func (c *Corretor) processarRequisicoesComWait() {
    defer c.wg.Done()
    c.processarRequisicoes()
}

func (c *Corretor) monitorarEleicaoComWait() {
    defer c.wg.Done()
    c.monitorarEleicao()
}

func (c *Corretor) monitorarFalhasComWait() {
    defer c.wg.Done()
    c.monitorarFalhas()
}
```

#### D. Refatorar `processarMensagensTCP` (linha ~128)
```go
func (c *Corretor) processarMensagensTCP() {
    defer func() {
        if r := recover(); r != nil {
            utils.RegistrarLog("ERRO", "Panic em processarMensagensTCP: %v", r)
        }
        if c.listenerTCP != nil {
            c.listenerTCP.Close()
        }
    }()
    
    for c.executando.Load() {  // ✅ Usar atomic.Bool
        select {
        case <-c.ctx.Done():
            utils.RegistrarLog("INFO", "processarMensagensTCP cancelado")
            return
        default:
        }
        
        conexao, err := c.listenerTCP.Accept()
        if err != nil {
            if c.executando.Load() {  // ✅ Usar atomic.Bool
                utils.RegistrarLog("ERRO", "Erro ao aceitar conexão TCP: %v", err)
            }
            continue
        }
        
        go c.tratarConexaoTCP(conexao)
    }
}
```

#### E. Refatorar `tratarSolicitacaoLock` (linha ~197)
```go
func (c *Corretor) tratarSolicitacaoLock(mensagem tipos.Mensagem) {
    // ✅ Processar e LIBERAR lock antes de I/O
    aprovado := c.mutexDistribuido.ProcessarSolicitacaoLock(mensagem)
    
    resposta := tipos.Mensagem{
        Tipo:         "RESPOSTA_LOCK",
        OrigemID:     c.id,
        DestinoID:    mensagem.OrigemID,
        Dados:        map[string]bool{"aprovado": aprovado},
        CarimboTempo: time.Now(),
    }
    
    // ✅ Operação I/O SEM LOCK
    vizinhosAtivos := c.estado.ObterVizinhosAtivos()
    if vizinho, existe := vizinhosAtivos[mensagem.OrigemID]; existe {
        if err := c.enviarRespostaTCP(vizinho.EnderecoTCP, resposta); err != nil {
            utils.RegistrarLog("AVISO", "Falha ao enviar resposta de lock: %v", err)
        }
    }
}
```

#### F. Refatorar `tratarRequisicao` (linha ~219)
```go
func (c *Corretor) tratarRequisicao(mensagem tipos.Mensagem) {
    dados, ok := mensagem.Dados.(map[string]interface{})
    if !ok {
        utils.RegistrarLog("ERRO", "Dados não são map[string]interface{}")
        return
    }
    
    tipo, tipoOk := dados["tipo"].(string)
    if !tipoOk {
        utils.RegistrarLog("ERRO", "Campo 'tipo' não é string")
        return
    }
    
    recursoID, recursoOk := dados["recurso_id"].(string)
    if !recursoOk {
        utils.RegistrarLog("ERRO", "Campo 'recurso_id' não é string")
        return
    }
    
    prioridade := 0
    if prioridadeRaw, existe := dados["prioridade"]; existe {
        switch v := prioridadeRaw.(type) {
        case float64:
            prioridade = int(v)
        case int:
            prioridade = v
        default:
            utils.RegistrarLog("AVISO", "Prioridade com tipo inválido: %T", v)
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
```

#### G. Refatorar `Parar` (linha ~470)
```go
func (c *Corretor) Parar() {
    c.mutex.Lock()
    if !c.executando.Load() {  // ✅ Usar atomic.Bool
        c.mutex.Unlock()
        return
    }
    c.executando.Store(false)  // ✅ Usar atomic.Bool
    c.mutex.Unlock()
    
    c.cancel()
    
    // ✅ Aguardar com timeout
    done := make(chan struct{})
    go func() {
        c.wg.Wait()
        close(done)
    }()
    
    select {
    case <-done:
        utils.RegistrarLog("INFO", "Todas as goroutines pararam")
    case <-time.After(5 * time.Second):
        utils.RegistrarLog("ERRO", "Timeout aguardando goroutines pararem")
    }
    
    if c.listenerTCP != nil {
        if err := c.listenerTCP.Close(); err != nil {
            utils.RegistrarLog("AVISO", "Erro ao fechar listener TCP: %v", err)
        }
    }
    
    c.gerenciadorBatimentos.Parar()
    c.protocoloGossip.Parar()
    c.filaDistribuida.Parar()
    
    if err := c.estado.SalvarEstado(); err != nil {
        utils.RegistrarLog("ERRO", "Falha ao salvar estado final: %v", err)
    }
    
    close(c.canalControle)
    utils.RegistrarLog("INFO", "Corretor %s parado", c.id)
}
```

---

## Fase 2: Arquivos Secundários

### 2.1 `internal/fila/fila.go`

**Adicionar WaitGroup:**
```go
type FilaDistribuida struct {
    id                   string
    canalProcessamento   chan *tipos.Requisicao
    mutex                sync.RWMutex
    fechado              atomic.Bool
    wg                   sync.WaitGroup        // ✅ NOVO
    ctx                  context.Context
    cancel               context.CancelFunc
}

func (f *FilaDistribuida) Parar() {
    f.fechado.Store(true)
    f.cancel()
    
    done := make(chan struct{})
    go func() {
        f.wg.Wait()
        close(done)
    }()
    
    select {
    case <-done:
        utils.RegistrarLog("INFO", "Fila parada corretamente")
    case <-time.After(5 * time.Second):
        utils.RegistrarLog("ERRO", "Timeout aguardando fila parar")
    }
    
    close(f.canalProcessamento)
}
```

### 2.2 `internal/eleicao/eleicao.go`

**Verificar para evitar race conditions com liderança**
```go
// Adicionar atomic.Bool para liderança
type AlgoritmoBully struct {
    idCorretor    string
    liderAtual    atomic.Value  // string
    // ...
}
```

### 2.3 `internal/exclusao_mutua/mutex.go`

**Garantir que locks são liberados rapidamente antes de I/O**

---

## 🧪 Testes de Validação

### 3.1 Teste de Race Condition
```bash
# Executar todo teste com race detector
go test -race ./...

# Ou específico do gossip
go test -race ./internal/gossip/...

# Com alto grau de paralelismo
GOMAXPROCS=16 go test -race ./...
```

### 3.2 Teste de Integração
```bash
# Limpar e reconstruir
docker-compose down -v
docker-compose build --no-cache

# Executar
docker-compose up

# Monitorar por 2 minutos
sleep 120

# Verifi erros
docker-compose logs | grep "ERRO\|panic\|race"

# Parar corretores um por um
docker stop corretor1
sleep 10
docker-compose logs | grep "ELEICAO"

docker stop corretor2
sleep 10
docker-compose logs | grep "detectado"
```

### 3.3 Teste de Stress
```bash
# Executar com muitos corretores
for i in {1..10}; do
    docker-compose scale corretor=$i
    sleep 5
    go test -race ./... &
done
wait
```

---

## 📊 Checklist de Implementação

### Fase 1: Imediato
- [ ] Copiar `batimento_corrigido.go` para `batimento.go`
- [ ] Refatorar `corretor.go` (A-G acima)
- [ ] Adicionar `atomic.Bool` em `processarMensagensTCP`
- [ ] Executar `go test -race ./...`
- [ ] Testar com Docker Compose

### Fase 2: Esta semana
- [ ] Refatorar `fila.go` com WaitGroup
- [ ] Refatorar `eleicao.go` com atomic
- [ ] Refatorar `exclusao_mutua.go`
- [ ] Executar testes de integração

### Fase 3: Próxima semana
- [ ] Adicionar testes unitários
- [ ] Documentar padrões
- [ ] Code review

---

## 🚀 Comandos Rápidos

```bash
# 1. Atualizar batimento.go
cp internal/gossip/batimento_corrigido.go internal/gossip/batimento.go

# 2. Testar race conditions
go test -race ./...

# 3. Rebuild docker
docker-compose build --no-cache

# 4. Executar
docker-compose up -d

# 5. Monitorar
docker-compose logs -f corretor1 | grep "ERRO"

# 6. Parar
docker-compose down -v
```

---

## ⚠️ Pontos Críticos

1. **NÃO** faça commit com `batimento.go` original
2. **SEMPRE** execute `go test -race` antes de push
3. **Verifique** logs de `ERRO` no docker-compose
4. **Teste** failover de líder manualmente
5. **Documente** todas as mudanças

---

## 📞 Suporte

Se encontrar problemas:
1. Consulte `ANALISE_CRITICA_ISSUES.md`
2. Execute `go test -race` para identificar race condition
3. Verifique `docker-compose logs -f` para panics
4. Compare com `batimento_corrigido.go` como referência
