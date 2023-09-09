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
  namespace = "default"

  group "task" {
    count = 1
    
    network {
      port "garbage_speak" {
        to = 1314
        host_network = "private"
      }
    }

    task "garbage_speak" {
      driver = "exec"

      config {
        command = "garbage-speak-${var.version}"
      }

      artifact {
        source = "s3://s3.us-east-005.backblazeb2.com/garbage-speak-application/garbage-speak-${var.version}"
        options {
          aws_access_key_id     = var.b2_access_key_id
          aws_access_key_secret = var.b2_secret_access_key
        }
      }

      template {
        data = <<EOF
        {{ with nomadVar "nomad/jobs/garbage_speak" }}POSTGRES_URL=postgresql://{{ .POSTGRES_USER }}:{{ .POSTGRES_PASSWORD }}{{end}}@{{ range nomadService "postgres" }}{{ .Address }}:{{ .Port }}{{ end }}/garbage_speak?sslmode=disable
{{ with nomadVar "nomad/jobs/garbage_speak" }}SMTP_PASSWORD={{ .SMTP_PASSWORD }}
SMTP_USERNAME={{ .SMTP_USERNAME }}
SMTP_HOST={{ .SMTP_HOST }}{{ end }}
GO_ENV=production
SITE_DOMAIN=garbagespeak.com
EOF
        destination = "local/env"
        env = true
      }

      env {
      }

      resources {
        cpu = 1024
        memory = 1024
      }

      service {
        port = "garbage_speak"
        name = "garbage-speak"
        provider = "nomad"
      }
    }
  }
}
