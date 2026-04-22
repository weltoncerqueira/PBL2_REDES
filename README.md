## 🚀 Executando o Sistema

### Pré-requisitos

- Docker 20.10+
- Docker Compose 2.0+
- Go 1.21+ (apenas para desenvolvimento local)

### Com Docker Compose (Recomendado)

#### Iniciar o Sistema

```bash
# Construir as imagens
docker-compose build

# Iniciar todos os corretores em background
docker-compose up -d

# Verificar status
docker-compose ps
```

#### Monitorar Logs

```bash
# Ver logs de todos os corretores (modo follow)
docker-compose logs -f

# Ver logs de um corretor específico
docker-compose logs -f corretor1
docker-compose logs -f corretor2
docker-compose logs -f corretor3

# Ver últimas 50 linhas
docker-compose logs --tail=50 corretor1

# Filtrar logs por evento
docker-compose logs | grep "líder"
docker-compose logs | grep "ELEICAO"
docker-compose logs | grep "ERRO"
```

#### Parar o Sistema

```bash
# Parar todos os corretores
docker-compose down

# Parar e remover dados persistidos
docker-compose down -v
```

### Desenvolvimento Local

#### Terminal 1 - Corretor 1
```bash
export BROKER_ID="corretor-001"
export PORT_TCP=":9000"
export PORT_UDP=":9001"
export PEERS="corretor-002,localhost:9002,localhost:9003,corretor-003,localhost:9004,localhost:9005"
go run cmd/corretor/main.go
```

#### Terminal 2 - Corretor 2
```bash
export BROKER_ID="corretor-002"
export PORT_TCP=":9002"
export PORT_UDP=":9003"
export PEERS="corretor-001,localhost:9000,localhost:9001,corretor-003,localhost:9004,localhost:9005"
go run cmd/corretor/main.go
```

#### Terminal 3 - Corretor 3
```bash
export BROKER_ID="corretor-003"
export PORT_TCP=":9004"
export PORT_UDP=":9005"
export PEERS="corretor-001,localhost:9000,localhost:9001,corretor-002,localhost:9002,localhost:9003"
go run cmd/corretor/main.go
```

## 🧪 Testando o Sistema

### Teste de Eleição de Líder

```bash
# 1. Identificar o líder atual
docker-compose logs | grep "se declarou líder"

# 2. Simular falha do líder (ex: corretor1)
docker stop corretor1

# 3. Observar nova eleição
docker-compose logs -f | grep "ELEICAO\|VITORIA"

# 4. Verificar novo líder
docker-compose logs --tail=20 | grep "líder"
```

### Teste de Failover e Recuperação

```bash
# Parar um corretor
docker stop corretor2

# Verificar detecção de falha
docker-compose logs | grep "detectado como falho"

# Recuperar corretor
docker start corretor2

# Verificar reintegração
docker logs -f corretor2
```

### Teste de Replicação de Estado

```bash
# Ver estado sincronizado
docker-compose logs | grep "Estado atualizado para versão"

# Ver recursos disponíveis
docker-compose logs | grep "Recursos disponíveis"
```

## 🔍 Comandos de Debug

### Inspeção de Containers

```bash
# Entrar em um container
docker exec -it corretor1 sh

# Ver processos
docker exec corretor1 ps aux

# Ver conexões de rede
docker exec corretor1 netstat -an
```

### Rede e Conectividade

```bash
# Ver rede Docker
docker network inspect sistema-distribuido-brokers_rede-corretores

# Ver IPs dos containers
docker inspect -f '{{.Name}} - {{.NetworkSettings.IPAddress}}' $(docker ps -q)

# Testar conectividade entre corretores
docker exec corretor1 ping corretor2
docker exec corretor1 nc -zv corretor2 9000
```

### Monitoramento de Recursos

```bash
# Ver uso de CPU e memória
docker stats

# Ver logs com timestamp
docker-compose logs -f --timestamps
```

## 📊 Script de Monitoramento

Crie um arquivo `monitorar.sh`:

```bash
#!/bin/bash

echo "🔍 Monitorando Sistema de Corretores Distribuídos"
echo "================================================"

while true; do
    clear
    echo "📊 Status dos Corretores:"
    docker-compose ps
    
    echo ""
    echo "👑 Líder Atual:"
    docker-compose logs --tail=50 | grep "se declarou líder" | tail -1
    
    echo ""
    echo "❤️ Batimentos Recentes:"
    docker-compose logs --tail=20 | grep "BATIMENTO" | tail -5
    
    echo ""
    echo "⚠️ Últimos Erros:"
    docker-compose logs --tail=30 | grep "ERRO\|AVISO" | tail -3
    
    sleep 5
done
```

Execute com:
```bash
chmod +x monitorar.sh
./monitorar.sh
```

## 🚨 Troubleshooting

### Problemas Comuns

```bash
# Erro de portas em uso
sudo lsof -i :9000  # Identifica processo
sudo kill -9 <PID>  # Libera porta

# Containers não iniciam
docker-compose down -v      # Remove tudo
docker-compose build --no-cache  # Reconstrói
docker-compose up -d        # Reinicia

# Logs muito verbosos
docker-compose logs --tail=100  # Limita linhas

# Verificar configuração
docker-compose config  # Valida docker-compose.yml
```

### Logs Esperados

```
[2026-04-21 10:15:30.123] [INFO] Corretor corretor-001 iniciando eleição
[2026-04-21 10:15:30.456] [INFO] Corretor corretor-002 recebeu mensagem de eleição
[2026-04-21 10:15:31.789] [INFO] Corretor corretor-001 se declarou líder
[2026-04-21 10:15:32.012] [INFO] Corretor corretor-002 reconhece corretor-001 como líder
[2026-04-21 10:15:35.678] [INFO] Batimento enviado para corretor-002
```

## 📁 Estrutura de Arquivos Gerados

```
/tmp/
├── corretor_estado_corretor-001.json  # Estado persistido corretor 1
├── corretor_estado_corretor-002.json  # Estado persistido corretor 2
└── corretor_estado_corretor-003.json  # Estado persistido corretor 3

./logs/ (se configurado)
├── corretor1.log
├── corretor2.log
└── corretor3.log
```