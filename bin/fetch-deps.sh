#!/bin/bash

HTMX_VERSION=1.9.2

curl -sL "https://unpkg.com/htmx.org@$HTMX_VERSION" > static/htmx.js
echo '<script type="text/javascript" src="/htmx.js"></script>' > layouts/partials/extended_head.html
