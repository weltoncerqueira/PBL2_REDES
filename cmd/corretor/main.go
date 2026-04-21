package main

import (
	//"fmt"
	"os"
	"os/signal"
	"sistema-distribuido-brokers/internal/corretor"
	"sistema-distribuido-brokers/pkg/utils"
	"syscall"
	"time"
)

func main() {
	// Obtém configurações do ambiente
	idCorretor := os.Getenv("BROKER_ID")
	if idCorretor == "" {
		idCorretor = utils.ObterIDUnico()
	}

	portaTCP := os.Getenv("PORT_TCP")
	if portaTCP == "" {
		portaTCP = ":9000"
	}

	portaUDP := os.Getenv("PORT_UDP")
	if portaUDP == "" {
		portaUDP = ":9001"
	}

	listaVizinhos := utils.AnalisarListaVizinhos(os.Getenv("PEERS"))

	// Configura logging
	utils.RegistrarLog("INFO", "Iniciando corretor %s", idCorretor)
	utils.RegistrarLog("INFO", "Porta TCP: %s, Porta UDP: %s", portaTCP, portaUDP)
	utils.RegistrarLog("INFO", "Vizinhos configurados: %d", len(listaVizinhos))

	// Cria e inicia o corretor
	c, err := corretor.NovoCorretor(idCorretor, portaTCP, portaUDP, listaVizinhos)
	if err != nil {
		utils.RegistrarLog("ERRO", "Falha ao criar corretor: %v", err)
		os.Exit(1)
	}

	if err := c.Iniciar(); err != nil {
		utils.RegistrarLog("ERRO", "Falha ao iniciar corretor: %v", err)
		os.Exit(1)
	}

	// Aguarda sinal de interrupção
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Loop principal - simula operação
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sigChan:
			utils.RegistrarLog("INFO", "Recebido sinal de interrupção, parando corretor...")
			c.Parar()
			return

		case <-ticker.C:
			// Exibe status periodicamente
			utils.RegistrarLog("INFO", "Status - Corretor: %s, Líder: %s",
				c.ObterID(), c.ObterLiderAtual())
		}
	}
}
