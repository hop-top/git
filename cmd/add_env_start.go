package cmd

// addEnvStartFlag / addNoEnvStartFlag hold the two halves of the
// --[no-]env-start pair. Each is only consulted when explicitly set; with
// neither given, GIT_HOP_AUTO_ENV_START and then hop.env.autoStart decide.
var (
	addEnvStartFlag   bool
	addNoEnvStartFlag bool
)
