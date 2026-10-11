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

// Identity returns the identity migration set.
func Identity() fs.FS {
	return mustSub("identity")
}

// Catalog returns the catalog migration set.
func Catalog() fs.FS {
	return mustSub("catalog")
}

// Payments returns the payments migration set.
func Payments() fs.FS {
	return mustSub("payments")
}

// Shipping returns the shipping migration set.
func Shipping() fs.FS {
	return mustSub("shipping")
}

// Invoicing returns the invoicing migration set.
func Invoicing() fs.FS {
	return mustSub("invoicing")
}

func mustSub(name string) fs.FS {
	sub, err := fs.Sub(files, "migrations/"+name)
	if err != nil {
		panic(err)
	}
	return sub
}
