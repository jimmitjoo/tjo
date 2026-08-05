# The tjo CLI

```
tjo new <name> [-t default|blog|api|saas] [-d postgres|mysql|mariadb|sqlite]
tjo make handler <Name>
tjo make controller <Name>          # resource controller
tjo make api-controller <Name>
tjo make model <Name>
tjo make migration <name>
tjo make auth                       # users, tokens, login, 2FA, remember-me, views, routes
tjo make session                    # session table migration
tjo make mail <name>
tjo make middleware <Name>
tjo migrate up|down|reset|to <n>
tjo run                             # build and run with live reload
tjo deploy                          # see docs/deploy
tjo mcp                             # Model Context Protocol server over stdio
tjo version
```

## Notes that matter

- `tjo new` clones the skeleton tag matching the CLI's own version. A dev build
  clones the default branch.
- `tjo make auth` writes the migration, the models, the middleware, the
  handlers, the views **and the routes**. If it cannot find its insertion point
  in `routes.go` it fails loudly rather than reporting success -- it used to
  match a marker that no longer existed and wrote no routes at all.
- `tjo mcp` exposes the generators over MCP. Tool names are namespaced `tjo_*`
  and the list order is stable, so a client can cache it and diff it.
