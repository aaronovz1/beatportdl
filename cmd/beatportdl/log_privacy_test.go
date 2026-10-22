package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"unspok3n/beatportdl/config"
)

func TestLogsRedactConfiguredCredentialsAndSignedURLs(t *testing.T) {
	var output bytes.Buffer
	app := &application{config: &config.AppConfig{Username: "fixture-user", Password: "fixture-password"}, logWriter: &output}
	message := "Get https://fixture-user:fixture-password@example.test/audio?X-Amz-Signature=private-signature&token=fixture-token: failed for fixture-user"
	app.LogError("download", errors.New(message))
	app.LogInfo(message)
	app.logWorkerPanic("fixture worker", message)
	for _, secret := range []string{"fixture-user", "fixture-password", "private-signature", "fixture-token"} {
		if strings.Contains(output.String(), secret) {
			t.Errorf("log contains %q: %s", secret, output.String())
		}
	}
	if !strings.Contains(output.String(), "example.test/audio") {
		t.Fatal("redaction removed useful request context")
	}
}
