---
title: Create Account
---

{{< html.inline >}}
<style>
  .auth-wrapper{
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .auth-wrapper .auth-form {
    max-width: 450px;
    width: 100%;
    padding: 0 20px;
  }

  .auth-wrapper h1{margin-bottom: 20px;}

  label {
   display: block;
   margin: 0.5rem 0;
  }
</style>

<div class="auth-wrapper">
  <div class="auth-form">
    <form hx-post="{{ .Site.Params.apiBaseUrl }}/users/create">
      <label for="username">Username</label>
      <input id="username" type="text" name="username" placeholder="desired username"/>

      <label for="email">Email (we do not spam or sell information)</label>
      <input id="email" type="email" name="email" placeholder="thot_ldr@email.com">

      <label for="password">Password</label>
      <input id="password type="text" name="password" placeholder="desired password">

      <label for="password_confirmation">Password Confirmation</label>
      <input id="password_confirmation" type="text" name="password_confirmation" placeholder="desired password again">
      <br>
      <br>
      <button class="btn btn-success">Create Account</button>

    </form>
  </div>
</div>
{{< /html.inline >}}


