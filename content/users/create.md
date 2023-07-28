---
title: Create Account
---

{{< html.inline >}}
<div class="auth-wrapper">
  <div class="auth-form">
    <form hx-post="{{ .Site.Params.apiBaseUrl }}/users/create">
      <label for="username">Username</label>
      <input id="username" type="text" name="username" placeholder="desired username"/>

      <label for="email">Email (we do not spam or sell information)</label>
      <input id="email" type="email" name="email" placeholder="thot_ldr@email.com">

      <label for="password">Password</label>
      <input id="password type="password" name="password" placeholder="desired password">

      <label for="password_confirmation">Password Confirmation</label>
      <input id="password_confirmation" type="text" name="password_confirmation" placeholder="desired password again">
      <br>
      <br>
      <button class="btn btn-success">Create Account</button>

    </form>
  </div>
</div>
{{< /html.inline >}}


