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
        to           = 1314
        host_network = "tailnet"
      }
    }

    task "garbage_speak" {
      driver = "exec"

      config {
        command = "./local/garbage-speak-${var.version}"
      }

      artifact {
        source = "s3://s3.us-east-005.backblazeb2.com/garbage-speak-application/garbage-speak-${var.version}"
        options {
          aws_access_key_id     = var.b2_access_key_id
          aws_access_key_secret = var.b2_secret_access_key
        }
      }

      template {
        data        = <<EOF
{{ with nomadVar "nomad/jobs" }}
POSTGRES_URL=postgresql://postgres:{{ .postgres_password }}{{end}}@{{ range nomadService "postgres" }}{{ .Address }}:{{ .Port }}{{ end }}/garbage_speak?sslmode=disable
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
