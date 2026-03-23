CREATE TYPE member_role AS ENUM ('owner', 'editor', 'viewer');

CREATE TABLE room_members (
    room_id   UUID REFERENCES rooms(id),
    user_id   UUID REFERENCES users(id),
    role      member_role NOT NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (room_id, user_id)
);