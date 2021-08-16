# yeetserv
A microservice to delete all rooms of a bridge that was shut down.

## Environment variables
* `LISTEN_ADDRESS` - The address to listen on. The docker image sets this to
  `:8080` by default.
* `SYNAPSE_URL` - The URL where the Synapse admin API is available.
* `ADMIN_ACCESS_TOKEN` - Access token for the Synapse admin API.
* `ASMUX_URL` - The URL where the client-server API is available. Access tokens
  in yeet requests are checked against this server. Defaults to using the same
  value as `SYNAPSE_URL`.
* `ASMUX_DATABASE_URL` - The URL to the asmux postgres database to get the
  room list from. Defaults to using `/joined_rooms` if not set.
* `ASMUX_MAIN_URL` - The URL where the asmux management API is available.
* `ASMUX_ACCESS_TOKEN` - Access token for the asmux management API.
* `REDIS_URL` - The URL to a redis database to persist the room deletion queue.
  Defaults to not persisting the queue if not set.
* `QUEUE_SLEEP` - How long to sleep between deleting rooms in seconds.
* `THREAD_COUNT` - Number of rooms to process simultaneously within each yeet
  request. Defaults to 5.
* `DRY_RUN` - If true, rooms won't actually be affected.

## API
The only endpoint is `POST /_matrix/client/unstable/com.beeper.yeetserv/clean_rooms`.
It doesn't take any request body, but requires an `Authorization` header with the
access token of the user whose rooms should be cleaned up. The service will then
fetch the list of rooms the user is in, filter away any rooms that aren't allowed
by [rules.go](rules.go) and call the [delete room API] for the rest.

The response from the endpoint will contain a JSON object that looks like this
(minus the comments):

```jsonc
{
  // Number of rooms that were successfully deleted
  "removed": 123,
  // Number of rooms that were filtered to be not deleted
  "skipped": 5,
  // Number of rooms that failed to be deleted
  "failed": 0
}
```

[delete room API]: https://matrix-org.github.io/synapse/latest/admin_api/rooms.html#delete-room-api
