---
title: New Garbage
---

{{< html.inline >}}
    <form hx-post="{{ .Site.Params.apiBaseUrl }}/garbage/new">
      <div hx-target="this" hx-swap="innerHTML">
        <label for="title">A brief summary of the garbage</label>
        <input
          id="title"
          autofocus
          rquired
          type="text"
          name="title"
          style="width: 100%"
          placeholder="Provide an explanation, title, or some context"/>

        <label for="garbage">Garbage Speak</label>
        <textarea id="garbage"
          required
          minlength="10"
          rows="3"
          autocorrect="off"
          wrap="soft"
          name="garbage"
          style="width: 100%"
          placeholder="This may be any garbage speak seen in the wild. Refer to the FAQ for what constitues garbage speak."></textarea>

        <label for="url">URL where seen (optional)</label>
        <input
          id="url"
          optional
          type="text"
          name="url"
          style="width: 100%"
          placeholder="A public URL where this garbage was seen"/>

        <label for="tags">Tags (optional)</label>
        <select id="tags" name="tags" multiple optional>
          <option>Nouned verb</option>
          <option>Verbed noun</option>
          <option>Nouned adjective</option>
          <option>Novel garbage</option>
          <option>Standard-issue garbage</option>
        </select>
        <br>
        <br>
        <button>Post</button>
      </div>
    </form>
{{< /html.inline >}}



