# GeoIP test fixtures

`country.mmdb` and `asn.mmdb` are MaxMind's published test databases, renamed to
match the `*country*.mmdb` / `*asn*.mmdb` lookup in `findDB`:

| File | Source |
|------|--------|
| `country.mmdb` | [`test-data/GeoLite2-Country-Test.mmdb`](https://github.com/maxmind/MaxMind-DB/blob/main/test-data/GeoLite2-Country-Test.mmdb) |
| `asn.mmdb` | [`test-data/GeoLite2-ASN-Test.mmdb`](https://github.com/maxmind/MaxMind-DB/blob/main/test-data/GeoLite2-ASN-Test.mmdb) |

They contain synthetic records for a handful of documentation networks — no real
subscriber data. From [maxmind/MaxMind-DB](https://github.com/maxmind/MaxMind-DB),
dual-licensed Apache-2.0 / MIT:

> Copyright (c) 2013-2024 MaxMind, Inc.
>
> Permission is hereby granted, free of charge, to any person obtaining a copy of
> this software and associated documentation files (the "Software"), to deal in
> the Software without restriction, including without limitation the rights to
> use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies
> of the Software, and to permit persons to whom the Software is furnished to do
> so, subject to the following conditions:
>
> The above copyright notice and this permission notice shall be included in all
> copies or substantial portions of the Software.
>
> THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
> IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
> FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
> AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
> LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
> OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
> SOFTWARE.
