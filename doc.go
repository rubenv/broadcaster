/*

Package broadcaster implements a websocket server for broadcasting Redis
pub/sub messages to web clients.

Originally based on https://github.com/rubenv/node-broadcast-hub but
significantly improved while moving to Go.

*/
package broadcaster

//go:generate godocdown -output README.md
