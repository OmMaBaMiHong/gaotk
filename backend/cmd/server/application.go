package main

import "github.com/Wei-Shaw/sub2api/internal/handler"

// Wire cleans module providers on initialization failure; the application also
// stops workers before closing its SQL pool during ordinary shutdown.
func initializeApplication(buildInfo handler.BuildInfo) (*Application, error) {
	app, cleanupProviders, err := injectApplication(buildInfo)
	if err != nil {
		return nil, err
	}
	cleanupApplication := app.Cleanup
	app.Cleanup = func() {
		cleanupProviders()
		cleanupApplication()
	}
	return app, nil
}
