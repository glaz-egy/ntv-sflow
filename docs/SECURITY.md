# Security Notes

## 1. Threat model

The application processes sensitive network metadata:
- internal IPs
- device names
- external destinations
- ports/protocols
- topology

Treat UI/API access as privileged.

## 2. Collector network

sFlow UDP listener:
- management network only where possible
- firewall source addresses to known exporters
- do not expose UDP/6343 publicly

sFlow itself should not be assumed authenticated/encrypted.

## 3. Web/API exposure

MVP may be trusted-network only.

Before remote exposure:
- authentication
- authorization
- TLS/reverse proxy
- session security
- CSRF considerations for mutating APIs
- audit changes to configuration

## 4. Geo origin privacy

Do not require exact home/street coordinates.

Support coarse origin precision:
- country
- region
- city
- custom approximate coordinate

UI should know/display precision.

## 5. Logging

Avoid verbose logs containing every observed flow in production.

Logs should emphasize:
- errors
- collector state
- aggregate system health

Debug raw flow logging:
- opt-in
- bounded
- documented as sensitive

## 6. Input safety

Collector:
- untrusted UDP input
- strict length validation
- no panic
- fuzz parser

API:
- validate filters
- cap query ranges/limits
- protect expensive historical queries

## 7. Configuration secrets

Never commit:
- DB passwords
- API secrets
- future auth secrets

Use environment/secret management.
