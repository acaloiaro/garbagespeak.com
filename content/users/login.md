---
title: Log in
---
{{< html.inline >}}
<div class="auth-wrapper">
  <div class="auth-form">
    <form hx-post="{{ .Site.Params.apiBaseUrl }}/users/login">
      <div hx-target="this" hx-swap="innerHTML">
        <label for="username">Username</label>
        <input id="username" type="text" name="username" placeholder="desired username" hx-post="{{ .Site.Params.apiBaseUrl }}/users/login">
      </div>

      <label for="password">Password</label>
      <input id="password" type="password" name="password" placeholder="desired password">

      <br>
      <br>
      <button class="btn btn-success">Log In</button>
    </form>
  </div>
</div>
{{< /html.inline >}}


