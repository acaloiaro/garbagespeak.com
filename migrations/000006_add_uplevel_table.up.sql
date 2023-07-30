CREATE TABLE IF NOT EXISTS uplevels(
  id uuid PRIMARY KEY default uuid_generate_v4(),
  garbage_id uuid NOT NULL,
  user_id uuid NOT NULL,
  created_at timestamp with time zone DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uplevel_user_id_garbage_id_idx ON public.uplevels USING btree (garbage_id, user_id);

