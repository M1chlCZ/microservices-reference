// Package migrations embeds the SQL migration sets of every service.
package migrations

import (
	"embed"
	"io/fs"
)

//go:embed migrations
var files embed.FS

// Orders returns the orders migration set.
func Orders() fs.FS {
	return mustSub("orders")
}

// Inventory returns the inventory migration set.
func Inventory() fs.FS {
	return mustSub("inventory")
}

// Notifications returns the notifications migration set.
func Notifications() fs.FS {
	return mustSub("notifications")
}

func mustSub(name string) fs.FS {
	sub, err := fs.Sub(files, "migrations/"+name)
	if err != nil {
		panic(err)
	}
	return sub
}
