# API Bin

[![HUMA Powered](https://img.shields.io/badge/Powered%20By-Huma-ff5f87)](https://huma.rocks/) [![Works With Restish](https://img.shields.io/badge/Works%20With-Restish-ff5f87)](https://rest.sh/)

Provides a simple, modern, example API for demoing or debugging various features, including:

- [OpenAPI 3](https://www.openapis.org/) & [JSON Schema](https://json-schema.org/)
- Client-driven content negotiation
  - `gzip` & `br` content encoding for large responses
  - `JSON`, `YAML`, & `CBOR` formats
- Conditional requests via `ETag` or `LastModified`
- Echo back request info to help debugging
- Cached responses to test proxy & client-side caching
- Example structured data
  - Shows off `object`, `array`, `string`, `date`, `binary`, `integer`, `number`, `boolean`, etc.
- Image responses `JPEG`, `WEBP`, `GIF`, `PNG` & `HEIC`
- [RFC7807](https://datatracker.ietf.org/doc/html/rfc7807) structured errors
