---
title: Log in
---
{{< html.inline >}}
<div class="auth-wrapper">
  <div class="auth-form">
    <form hx-post="{{ .Site.Params.apiBaseUrl }}/users/login">
      <label for="username">Username</label>
      <input id="username" type="text" name="username" placeholder="your username">

      <label for="password">Password</label>
      <input id="password" type="password" name="password" placeholder="your password">

      <br>
      <br>
      <button>Log In</button>
    </form>
  </div>
</div>
{{< /html.inline >}}


