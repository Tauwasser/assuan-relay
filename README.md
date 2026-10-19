# Assuan Socket Relay

This program relays an Assuan socket on Windows as a plain TCP endpoint.

On Windows, GnuPG components such as `gpg-agent`, `scdaemon`, or `dirmngr`
cannot use Unix domain sockets for inter-process communication the way they do
on POSIX systems.
Instead, libassuan writes a small socket file that contains the decimal TCP port
of the server, followed by a newline and a 16-byte binary nonce.

A client that wants to talk to the agent reads that file, connects to
`127.0.0.1:<port>`, and must send the nonce as the very first payload on the
connection. Then the actual, client-specific protocol follows.
However, Linux clients currently do not expect to talk to a Windows socket this
way and SSH cannot forward these types of sockets from Windows to Linux.

`assuan-relay` removes that burden — it listens on a fixed TCP port, re-reads
the socket file for every accepted connection, performs the nonce handshake, and
then relays traffic transparently in both directions.

The security relies on the access rights of the socket file itself, binding to
the IPv4 loopback address only and having the client present the nonce on every
connection.

> [!CAUTION]  
> `assuan-relay` running in a multi-user environment is therefore unsafe as
> every concurrent user could simply connect to the exposed TCP port and act
> with the same rights as the user running `assuan-relay`.

## Intended Use Case

The intended use case is forwarding the GPG Extra Agent socket from GnuPG on
Windows to Linux on a single-user machine via SSH in `.ssh/config`:

```
Host example
  HostName 2001:db8::1
  User example
  IdentityFile "C:\Users\example\.ssh\id_ed25519"
  IdentitiesOnly yes
  Port 22
  RemoteForward /run/user/1000/gnupg/S.gpg-agent 127.0.0.1:8127
```

Then simply run `assuan-relay 8127 "%LOCALAPPDATA%\gnupg\S.gpg-agent.extra" on
the local machine running SSH.

## Usage

```
assuan-relay <tcp-port> <assuan-socket-file>
```

For example, to expose the GnuPG extra agent on port 8270:

```
assuan-relay 8270 "%LOCALAPPDATA%\gnupg\S.gpg-agent.extra"
```

The listener binds to `127.0.0.1` (IPv4 loopback), because binding to both IPv4
and IPv6 is unnecessary for the SSH forwarding scenario described above.
Original Assuan sockets also only bind to IPv4 loopback, so this shouldn't be
an issue.

> [!WARNING]  
> Never expose/forward plain `"%LOCALAPPDATA%\gnupg\S.gpg-agent"`, because it
> allows unsafe operations such as exporting private keys by default.

## Building

```
go build -o build/assuan-relay.exe .
```

Requires Go 1.27. There are no external dependencies.

## Tests

```
go test
```

The test suite covers socket-file parsing (including edge cases such as
CRLF line endings, trailing data, and malformed files) and a full end-to-end
relay against a fake Assuan server that verifies the nonce arrives as the
first payload.

## License

Code is licensed under MIT, see [LICENSE](./LICENSE) for more details.
