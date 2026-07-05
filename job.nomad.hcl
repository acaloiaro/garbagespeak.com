variable "version" {
  type = string
}

variable "b2_access_key_id" {
  type = string
}

variable "b2_secret_access_key" {
  type = string
}

job "garbage_speak" {
  datacenters = ["dc1"]
  namespace   = "default"

  group "task" {
    count = 1

    network {
      port "garbage_speak" {
        to = 1314
      }
    }

    task "db-init" {
      driver = "podman"
      user   = "root"

      lifecycle {
        hook    = "prestart"
        sidecar = false
      }

      config {
        image        = "docker.io/postgres:17"
        command      = "/bin/sh"
        args         = ["${NOMAD_TASK_DIR}/provision.sh"]
        network_mode = "host"
      }

      template {
        destination = "${NOMAD_SECRETS_DIR}/env.vars"
        env         = true
        data        = <<EOF
{{ with nomadVar "nomad/jobs" -}}
PGHOST={{ .postgres_host }}
PGPORT={{ .postgres_port }}
PGUSER={{ .postgres_user }}
PGPASSWORD={{ .postgres_password }}
PGSSLMODE=require
{{- end }}
{{ with nomadVar "nomad/jobs/garbage_speak" -}}
POSTGRES_APP_PASSWORD={{ .db_password }}
{{- end }}
EOF
      }

      template {
        destination = "${NOMAD_TASK_DIR}/provision.sh"
        perms       = "755"
        data        = <<EOF
#!/bin/sh
set -eu

DB_NAME="{{ env "NOMAD_JOB_NAME" }}"
ROLE_NAME="{{ env "NOMAD_JOB_NAME" | replaceAll "-" "_" }}_app"

psql -d defaultdb <<'SQL'
  SELECT 'CREATE DATABASE garbage_speak
    ENCODING ''UTF8''
    LC_COLLATE ''C''
    LC_CTYPE ''C''
    TEMPLATE template0'
  WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'garbage_speak')\gexec
SQL

psql -d defaultdb \
  -c "DO \$\$
      BEGIN
        IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '$ROLE_NAME') THEN
          CREATE ROLE $ROLE_NAME WITH LOGIN PASSWORD '${POSTGRES_APP_PASSWORD}';
        ELSE
          ALTER ROLE $ROLE_NAME WITH PASSWORD '${POSTGRES_APP_PASSWORD}';
        END IF;
      END
      \$\$;
      GRANT ALL PRIVILEGES ON DATABASE $DB_NAME TO $ROLE_NAME;"

psql -d "$DB_NAME" -c "GRANT ALL ON SCHEMA public TO $ROLE_NAME;" \
  || psql -d "$DB_NAME" -c "SET ROLE pg_database_owner; GRANT ALL ON SCHEMA public TO $ROLE_NAME; RESET ROLE;" \
  || true

psql -d "$DB_NAME" \
  -c "REASSIGN OWNED BY $PGUSER TO $ROLE_NAME;
      ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO $ROLE_NAME;
      ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO $ROLE_NAME;"
EOF
      }

      resources {
        cpu    = 128
        memory = 128
      }
    }

    task "garbage_speak" {
      driver = "exec"

      config {
        command = "./local/garbage-speak-${var.version}"
      }

      artifact {
        source      = "s3::https://s3.us-east-005.backblazeb2.com/garbage-speak-application/garbage-speak-${var.version}"
        destination = "local/garbage-speak-${var.version}"
        mode        = "file"
        options {
          aws_access_key_id     = var.b2_access_key_id
          aws_access_key_secret = var.b2_secret_access_key
        }
      }

      template {
        data        = <<EOF
{{ with nomadVar "nomad/jobs" -}}
POSTGRES_URL=postgresql://{{ env "NOMAD_JOB_NAME" | replaceAll "-" "_" }}_app:{{ with nomadVar "nomad/jobs/garbage_speak" }}{{ .db_password }}{{ end }}@{{ .postgres_host }}:{{ .postgres_port }}/{{ env "NOMAD_JOB_NAME" }}?sslmode=require
{{- end }}
{{ with nomadVar "nomad/jobs/garbage_speak" }}SMTP_PASSWORD={{ .SMTP_PASSWORD }}{{ end }}
EOF
        destination = "local/env"
        env         = true
      }

      env {
        SMTP_USERNAME          = "postmaster@garbagespeak.com"
        SMTP_HOST              = "smtp.mailgun.org:587"
        GO_ENV                 = "production"
        SITE_DOMAIN            = "garbagespeak.com"
        GOOGLE_OAUTH_CLIENT_ID = "1038207836187-uivhpqmd2aps0q61hr0q62k6vcgghegc.apps.googleusercontent.com"
      }

      resources {
        cpu    = 128
        memory = 64
      }

      service {
        port         = "garbage_speak"
        name         = "garbage-speak"
        provider     = "nomad"
        address_mode = "host"
      }
    }
  }
}
