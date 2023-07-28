#!/bin/bash

HTMX_VERSION=1.9.2

mkdir -p static/img

curl -sL "https://unpkg.com/htmx.org@$HTMX_VERSION" > static/htmx.js
curl -sL "https://unpkg.com/htmx.org/dist/ext/remove-me.js" > static/remove-me.js
curl -sL "https://htmx.org/img/bars.svg" > static/img/bars.svg

echo '<script type="text/javascript" src="/htmx.js"></script>' > layouts/partials/extended_head.html
echo '<script type="text/javascript" src="/remove-me.js"></script>' >> layouts/partials/extended_head.html
echo "<meta name=\"htmx-config\" content='{\"withCredentials\": true}'>" >> layouts/partials/extended_head.html
