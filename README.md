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
* `FORCE_PURGE` - If true, rooms will be purged regardless of whether the host
  still has users in the room.

## API
### Clean all rooms of a bridge
`POST /_matrix/client/unstable/com.beeper.yeetserv/clean_all` can be used to
clean up all rooms owned by a specific bridge. It doesn't take any request body,
but requires an `Authorization` header with the `as_token` of the bridge whose
rooms should be cleaned up.

The service will then:
1. Fetch the list of rooms (either from the asmux database, or using
   `/joined_rooms` if `ASMUX_DATABASE_URL` is not set).
2. Filter away any rooms that aren't allowed by [rules.go](rules.go).
3. Force any non-bridge users to leave the room (using the admin API to get an
   access token for that user and calling the normal `/leave` endpoint).
4. Queue the rooms for deletion.

There's a background loop that consumes a single room ID from the queue every X
seconds (defined by `QUEUE_SLEEP`) and then deletes that room using the [delete
room API]. If `ASMUX_MAIN_URL` and `ASMUX_ACCESS_TOKEN` are set, it will
also tell asmux to forget about the room.

[delete room API]: https://matrix-org.github.io/synapse/latest/admin_api/rooms.html#delete-room-api

The response from the endpoint will contain a JSON object that looks like this
(minus the comments):

```jsonc
{
  // Number of rooms that were successfully queued for deletion.
  "removed": 123,
  // Number of rooms that were filtered to be not deleted.
  "skipped": 5,
  // Number of rooms that failed to be deleted (either the kick or the queuing failed).
  "failed": 0
}
```

### Queue individual rooms for cleanup
Bridges can use `POST /_matrix/client/unstable/com.beeper.yeetserv/queue` to add
individual rooms to the cleanup queue. The endpoint requires the same auth as
the `/clean_all` endpoint and does the same checks, but does not make users
leave (this can be implemented later if necessary).

The endpoint takes a JSON body with a list of room IDs:
```json
{
  "room_ids": ["!foo:example.com", "!bar:example.com"]
}
```

It will return the room IDs split into three categories:

```jsonc
{
  // Rooms that were successfully queued for deletion.
  "queued": ["!foo:example.com"],
  // Rooms that failed to be queued. This shouldn't happen unless there are
  // internal errors, but retrying might work.
  "failed": [],
  // Rooms that were rejected by the filter.
  "rejected": ["!bar:example.com"]
}
```
