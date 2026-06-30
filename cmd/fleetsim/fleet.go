package main

// node is one fleet member: its name and last-known full JSON body.
type node struct {
	name string
	body []byte
}
