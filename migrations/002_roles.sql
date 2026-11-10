INSERT OR IGNORE INTO roles (id, name) VALUES
    ('00000000-0000-4000-8000-0000000000a2', 'admin'),
    ('00000000-0000-4000-8000-0000000000a3', 'manager'),
    ('00000000-0000-4000-8000-0000000000a4', 'client');

INSERT OR IGNORE INTO users (id, name, email, phone, role_id)
VALUES (
    '00000000-0000-4000-8000-0000000000a1',
    'Администратор',
    'admin@example.local',
    '+70000000000',
    '00000000-0000-4000-8000-0000000000a2'
);
