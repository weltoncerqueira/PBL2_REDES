package utils

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// ObterIDUnico gera um identificador único
func ObterIDUnico() string {
	hostname, _ := os.Hostname()
	return fmt.Sprintf("%s-%d", hostname, time.Now().UnixNano())
}

// SerializarMensagem converte uma mensagem para bytes
func SerializarMensagem(msg interface{}) ([]byte, error) {
	return json.Marshal(msg)
}

// DesserializarMensagem converte bytes para mensagem
func DesserializarMensagem(dados []byte, msg interface{}) error {
	return json.Unmarshal(dados, msg)
}

// ObterEnderecoLocal obtém o endereço IP local
func ObterEnderecoLocal() string {
	conexao, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conexao.Close()

	endereco := conexao.LocalAddr().(*net.UDPAddr)
	return endereco.IP.String()
}

// AnalisarListaVizinhos processa a string de vizinhos do ambiente
func AnalisarListaVizinhos(listaVizinhos string) []string {
	if listaVizinhos == "" {
		return []string{}
	}
	return strings.Split(listaVizinhos, ",")
}

// RegistrarLog registra uma mensagem de log com timestamp
func RegistrarLog(nivel, mensagem string, args ...interface{}) {
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	fmt.Printf("[%s] [%s] %s\n", timestamp, nivel, fmt.Sprintf(mensagem, args...))
}
