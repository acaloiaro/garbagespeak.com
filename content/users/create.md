---
title: Create Account
---

{{< html.inline >}}
<div class="auth-wrapper">
  <div class="auth-form">
    <form hx-post="{{ .Site.Params.apiBaseUrl }}/users/create">
      <div hx-target="this" hx-swap="innerHTML">
        <label for="username">Username</label>
        <input id="username"
          type="text"
          name="username"
          placeholder="desired username"
          hx-post="{{ .Site.Params.apiBaseUrl }}/users/new_user_validation"
          hx-indicator="#username-ind">
        <img id="username-ind" src="/img/bars.svg" class="htmx-indicator"/>

        <label for="email">Email (we do not spam or sell information)</label>
        <input
          id="email"
          type="email"
          name="email"
          placeholder="thot_ldr@email.com"
          hx-post="{{ .Site.Params.apiBaseUrl }}/users/new_user_validation"
          hx-indicator="#email-ind">
        <img id="email-ind" src="/img/bars.svg" class="htmx-indicator"/>

        <label for="password">Password</label>
        <input
          id="password
          type="password"
          name="password"
          placeholder="desired password"
          hx-post="{{ .Site.Params.apiBaseUrl }}/users/new_user_validation"
          hx-indicator="#password-ind">
        <img id="password-ind" src="/img/bars.svg" class="htmx-indicator"/>

        <label for="password_confirmation">Password Confirmation</label>
        <input
          id="password_confirmation"
          type="text"
          name="password_confirmation"
          placeholder="desired password again"
          hx-post="{{ .Site.Params.apiBaseUrl }}/users/new_user_validation"
          hx-indicator="#password-conf-ind">
        <img id="password-conf-ind" src="/img/bars.svg" class="htmx-indicator"/>

        <br>
        <br>
        <button disabled>Create Account</button>
      </div>
    </form>
  </div>
</div>
{{< /html.inline >}}


