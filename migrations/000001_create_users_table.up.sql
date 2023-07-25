CREATE TABLE IF NOT EXISTS users(
  id bigserial PRIMARY KEY,
  username VARCHAR (50) UNIQUE NOT NULL,
  password VARCHAR (50) NOT NULL,
  email VARCHAR (300) UNIQUE NOT NULL,
  created_at timestamp with time zone DEFAULT now(),
  updated_at timestamp with time zone
);

CREATE UNIQUE INDEX IF NOT EXISTS users_id_idx ON public.users USING btree (id);

