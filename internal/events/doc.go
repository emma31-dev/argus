// Package events reads the intents.near event stream from FastNEAR's
// neardata block service and turns it into model.IncidentEvent rows.
//
// It has four layers: ParseLog decodes one EVENT_JSON log line (NEP-245 and
// DIP-4), Client fetches blocks, Extract picks the intents.near events out of
// a block, and Follower/Backfill/Attacher drive them. See README.md for what
// is verified and what is assumed.
package events
