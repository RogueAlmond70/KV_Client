# KV Client

Go client for the encrypted key-value store. The server encrypts each value. This client sends plaintext bytes. `store` prints the key the server returned as standard base64.

The server listens on port `8080` for both the in-memory store and Postgres. `docker compose up --build` starts the in-memory server. To use Postgres instead, from the server directory:

```powershell
$env:DATASTORE_TYPE = "POSTGRES"
docker compose --profile postgres up --build
```

The client uses the same address either way. Postgres keeps entries in the `pgdata` volume. Restarting the in-memory server drops its entries.

## Install

```sh
go get github.com/RogueAlmond70/KV_Client
```

Requires Go 1.27.

## Command

To test our encrypted key value server I've created this simple client. The client is CLI based, and accepts a file path to a json file, which can be used as the request. For simplicity's sake, I've provided three sample json files in the root directory of this client, which can be used for the `store`, and `put` (update) operations. The `delete` and `get` operations only require the id and the key.

Each sample json file has three fields, including a key field, which has been intentionally left blank so it can be used for the initial `store` operation, as no key is required for the `store` operation:

```json
{
"id": "001",
"data": "This is the first user.. hey",
"key": ""
}
```

`store` needs `id` and `data`. A string `data` is stored as those characters. Any other JSON value is stored as its own JSON text, whitespace included.

From this directory, each command takes one JSON file. `-addr` comes before the command and defaults to `http://localhost:8080`. On success the command prints a status line to stderr. The key from `store` and the plaintext from `get` stay on stdout. 

To store the user001.json file from the root directory, for example, we run the following from the powershell:

```powershell
go run ./cmd/kv store .\user001.json
```

Running the `store` command will print the encryption key to the terminal as standard base64. The CLI does not save keys. Hold onto this key as all other commands (`put` (update), `delete`, `get`) will require this value in the `key` field of the JSON file. The client decodes that field and sends it in the `X-Encryption-Key` header. `get` and `delete` send the header and no body. `put` sends the header and a body that contains only `data`.



`get` and `delete` need `id` and `key`. `get` writes the stored plaintext to stdout. The server decrypts the value before it responds.

```json
{
  "id": "001",
  "key": "<key from store>"
}
```

```powershell
go run ./cmd/kv get .\user001.json
go run ./cmd/kv delete .\user001.json
```

`put` needs `id`, `key`, and `data` in the same file.

```json
{
  "id": "001",
  "key": "<key from store>",
  "data": "world"
}
```

```powershell
go run ./cmd/kv put .\user001.json
```

Those commands talk to port `8080`. Pass `-addr` before the command only when the server is published somewhere else.

A bad JSON file, a missing field, or a blank `key` on `get`, `put`, or `delete` is rejected by the CLI and is not sent. The process exits 2.

| Status | Meaning |
| --- | --- |
| 400 | Missing id, missing data, or a key header that is not standard base64 |
| 401 | Id exists and the key does not match |
| 404 | Id is not in the store |
| 409 | `store` for an id that already exists |
| 500 | The body is `failed to handle request` |
