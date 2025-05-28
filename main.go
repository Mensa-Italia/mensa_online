package main

import (
	"io/ioutil"
	"log"
	"mensadb/tools/aipower"
)

func main() {
	lines := [5]string{
		"GIOVEDÌ 5 GIUGNO",
		"ORE 19:45",
		"CINGHIALE E VINO",
		"VIAENZO FERRARI 32",
		"MONCALIERI (TO)",
	}

	imgBytes, err := aipower.GenerateEventCard(
		"CENA A TORINO", lines)
	if err != nil {
		log.Fatalf("Errore: %v", err)
	}

	if err := ioutil.WriteFile("output.png", imgBytes, 0644); err != nil {
		log.Fatalf("Impossibile salvare: %v", err)
	}
}
