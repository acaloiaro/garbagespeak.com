CREATE TABLE IF NOT EXISTS garbages(
  id bigserial PRIMARY KEY,
  owner_id bigint,
  title text NOT NULL,
  content text NOT NULL,
  metadata jsonb default '{}'::jsonb,
  created_at timestamp with time zone default now(),
  updated_at timestamp with time zone
);

ALTER TABLE ONLY public.garbages ADD CONSTRAINT onwer_id_fkey FOREIGN KEY (owner_id) REFERENCES public.users(id) ON DELETE CASCADE;
