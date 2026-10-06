package main

import (
	"github.com/EdevandoAlves/backend-go/internal/app"
	"go.uber.org/fx"
)

func main() {
	fx.New(app.Module()).Run()
}
