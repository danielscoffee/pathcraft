// Package engine provides PathCraft's public Go routing facade.
//
// An Engine loads OSM and optional GTFS data, then answers street, transit,
// and multimodal route requests. Fully loaded engines support concurrent
// read-only queries; loading, mutation, and hot reload must remain serial.
package engine
