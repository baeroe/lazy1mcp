# Edit form: command, args, tags, timeout and the env variables as a key/value list.
Output ".vhs/form.gif"
Source "docs/tapes/config.tape"

Hide
Type "lazy1mcp" Enter
Sleep 2.5s
Down 4
Sleep 1s
Type "e"
Sleep 300ms
Tab 5
Sleep 300ms
Tab 4
Type "LOG_LEVEL"
Tab
Type "debug"
Sleep 500ms
Show
Sleep 300ms
Screenshot "docs/screenshots/form.png"
Sleep 500ms
