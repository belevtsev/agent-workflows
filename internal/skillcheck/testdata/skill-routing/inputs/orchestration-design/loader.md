# Plugin loader today

```go
type Plugin interface {
	Name() string
	Run(ctx context.Context, args []string) error
}
```

Plugins are compiled into the binary and registered in `loader.Register`. There is no version field and no isolation between plugins.
