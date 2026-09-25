package cli

// InstallUsageErrors exposes installUsageErrors to the external test
// package, which walks the real command tree registered by package cmd.
var InstallUsageErrors = installUsageErrors

// ShorthandDomain exposes shorthandDomain to the external test package.
var ShorthandDomain = shorthandDomain
