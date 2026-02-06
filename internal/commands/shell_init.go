package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ShellInit outputs a shell function that wraps the ralph binary,
// intercepting workspace commands to handle directory changes natively.
func ShellInit(args []string) error {
	shellPath := os.Getenv("SHELL")
	return shellInit(shellPath, os.Stdout)
}

// shellInit generates the shell function for the given shell and writes it to w.
func shellInit(shellPath string, w io.Writer) error {
	shellName := filepath.Base(shellPath)

	switch shellName {
	case "bash", "zsh":
		_, err := fmt.Fprint(w, shellFunction())
		return err
	default:
		return fmt.Errorf("Currently only bash and zsh are supported. Detected: %s", shellPath)
	}
}

func shellFunction() string {
	return `ralph() {
    export RALPH_SHELL_INIT=1

    case "$1" in
        workspaces)
            case "$2" in
                new)
                    __output=$(command ralph "$@")
                    __exit=$?
                    if [ $__exit -ne 0 ]; then
                        return $__exit
                    fi
                    __path=$(echo "$__output" | tail -n 1)
                    if [ -n "$__path" ] && [ -d "$__path" ]; then
                        cd "$__path" || return 1
                        export RALPH_WORKSPACE="$3"
                        if [ ! -f "../prd.json" ]; then
                            command ralph prd new
                        fi
                    fi
                    ;;
                switch)
                    __output=$(command ralph "$@")
                    __exit=$?
                    if [ $__exit -ne 0 ]; then
                        return $__exit
                    fi
                    __path=$(echo "$__output" | tail -n 1)
                    if [ -n "$__path" ] && [ -d "$__path" ]; then
                        cd "$__path" || return 1
                        if [ -n "$3" ]; then
                            if [ "$3" = "base" ]; then
                                unset RALPH_WORKSPACE
                            else
                                export RALPH_WORKSPACE="$3"
                            fi
                        fi
                    fi
                    ;;
                remove)
                    __output=$(command ralph "$@")
                    __exit=$?
                    if [ $__exit -ne 0 ]; then
                        return $__exit
                    fi
                    __path=$(echo "$__output" | tail -n 1)
                    if [ -n "$__path" ] && [ -d "$__path" ]; then
                        cd "$__path" || return 1
                        unset RALPH_WORKSPACE
                    fi
                    ;;
                prune)
                    __output=$(command ralph "$@")
                    __exit=$?
                    if [ $__exit -ne 0 ]; then
                        return $__exit
                    fi
                    __path=$(echo "$__output" | tail -n 1)
                    if [ -n "$__path" ] && [ -d "$__path" ]; then
                        cd "$__path" || return 1
                        unset RALPH_WORKSPACE
                    fi
                    ;;
                *)
                    command ralph "$@"
                    ;;
            esac
            ;;
        switch)
            __output=$(command ralph "$@")
            __exit=$?
            if [ $__exit -ne 0 ]; then
                return $__exit
            fi
            __name=$(echo "$__output" | head -n 1)
            __path=$(echo "$__output" | tail -n 1)
            if [ -n "$__path" ] && [ -d "$__path" ]; then
                cd "$__path" || return 1
                if [ "$__name" = "base" ]; then
                    unset RALPH_WORKSPACE
                else
                    export RALPH_WORKSPACE="$__name"
                fi
            fi
            ;;
        done)
            __output=$(command ralph "$@")
            __exit=$?
            if [ $__exit -ne 0 ]; then
                return $__exit
            fi
            __path=$(echo "$__output" | tail -n 1)
            if [ -n "$__path" ] && [ -d "$__path" ]; then
                cd "$__path" || return 1
                unset RALPH_WORKSPACE
            fi
            ;;
        *)
            command ralph "$@"
            ;;
    esac
}
`
}
