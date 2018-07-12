package main

import (
	"github.com/bugsnag/bugsnag-go"
	"github.com/fox-one/ocean.one/config"
)

func setupBugsnag() {
	logger := &bugsnagLogger{}
	bugsnag.Configure(bugsnag.Configuration{
		APIKey:              config.BugsnagAPIKey,
		AppVersion:          config.BuildVersion,
		ReleaseStage:        config.Environment,
		NotifyReleaseStages: []string{"development", "staging", "production"},
		PanicHandler:        func() {},
		Logger:              logger,
	})
}

type bugsnagLogger struct{}

func (logger *bugsnagLogger) Printf(format string, v ...interface{}) {
}
