package main

import (
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"log"
)

var app = pocketbase.New()

func main() {

	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		Dir:         "./migrations",
		Automigrate: true,
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
