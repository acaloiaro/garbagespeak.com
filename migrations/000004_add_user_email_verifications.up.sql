CREATE TABLE IF NOT EXISTS user_email_verifications(
  id uuid PRIMARY KEY default uuid_generate_v4(),
  user_id uuid NOT NULL,
  created_at timestamp with time zone DEFAULT now(),
  updated_at timestamp with time zone
);

CREATE UNIQUE INDEX IF NOT EXISTS user_email_verifications_id_idx ON public.user_email_verifications USING btree (id);

