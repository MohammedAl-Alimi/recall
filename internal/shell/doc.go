// Package shell manages the shell widget that binds Ctrl-G to
// 'recall --from-widget' in zsh, bash and fish.
//
// The snippet is written between the markers '# >>> recall >>>' and
// '# <<< recall <<<' so Install and Uninstall are idempotent. The alias rcl
// is only added when the name is free.
//
// Rules:
//   - Only the rc file passed in is touched. Tests use a temp rc file.
package shell
