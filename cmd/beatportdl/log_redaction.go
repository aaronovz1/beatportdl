package main

import "unspok3n/beatportdl/internal/beatport"

func (app *application) redactLog(message string) string {
	message = app.bp.RedactLog(message)
	if app.config != nil {
		return beatport.SanitizeLogMessage(message, app.config.Username, app.config.Password)
	}
	return message
}
