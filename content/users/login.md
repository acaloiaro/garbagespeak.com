---
title: Log in
---
{{< html.inline >}}
<div class="auth-wrapper">
  <div class="auth-form">
    <form hx-post="{{ .Site.Params.apiBaseUrl }}/users/login">
      <div hx-target="this" hx-swap="outerHTML">
        <label for="username">Username</label>
        <input id="username" type="text" name="username" placeholder="desired username" hx-post="{{ .Site.Params.apiBaseUrl }}/users/login">
        <img id="ind" src="/img/bars.svg" class="htmx-indicator"/>
        <label for="password">Password</label>
        <input id="password" type="password" name="password" placeholder="desired password">

        <br>
        <br>
        <button class="btn btn-success">Log In</button>
      </div>
    </form>
  </div>
</div>
{{< /html.inline >}}


