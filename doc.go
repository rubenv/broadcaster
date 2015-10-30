/*

Package broadcaster implements a websocket server for broadcasting Redis
pub/sub messages to web clients.

A JavaScript client can be found here: https://github.com/rubenv/broadcaster-client

Originally based on https://github.com/rubenv/node-broadcast-hub but
significantly improved while moving to Go.

*/
package broadcaster

//go:generate godocdown -output README.md
