CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS users(
  id uuid PRIMARY KEY default uuid_generate_v4(),
  username VARCHAR (20) UNIQUE NOT NULL,
  password VARCHAR (100) NOT NULL,
  email TEXT UNIQUE NOT NULL,
  created_at timestamp with time zone DEFAULT now(),
  updated_at timestamp with time zone
);

CREATE UNIQUE INDEX IF NOT EXISTS users_id_idx ON public.users USING btree (id);

