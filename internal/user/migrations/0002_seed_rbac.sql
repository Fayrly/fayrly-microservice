-- Permission catalogue. Keys are "<resource>:<action>" and are embedded in
-- access tokens, so treat them as a public API contract.
INSERT INTO permissions (id, key, resource, action, description) VALUES
('00000000-0000-4000-8000-000000000101', 'user:read',    'users',   'read',    'View user accounts'),
('00000000-0000-4000-8000-000000000102', 'user:write',   'users',   'write',   'Create and update user accounts, reset passwords'),
('00000000-0000-4000-8000-000000000103', 'user:delete',  'users',   'delete',  'Deactivate and delete user accounts'),
('00000000-0000-4000-8000-000000000104', 'role:read',    'roles',   'read',    'View roles, permissions and the access matrix'),
('00000000-0000-4000-8000-000000000105', 'role:write',   'roles',   'write',   'Create roles and change the access matrix'),
('00000000-0000-4000-8000-000000000106', 'event:read',   'events',  'read',    'View events including unpublished ones'),
('00000000-0000-4000-8000-000000000107', 'event:write',  'events',  'write',   'Create and edit events'),
('00000000-0000-4000-8000-000000000108', 'event:delete', 'events',  'delete',  'Delete events'),
('00000000-0000-4000-8000-000000000109', 'event:publish','events',  'publish', 'Publish events and move them through the lifecycle'),
('00000000-0000-4000-8000-000000000110', 'report:read',  'reports', 'read',    'View reports'),
('00000000-0000-4000-8000-000000000111', 'report:write', 'reports', 'write',   'Create, edit and submit reports'),
('00000000-0000-4000-8000-000000000112', 'report:delete','reports', 'delete',  'Delete draft or rejected reports'),
('00000000-0000-4000-8000-000000000113', 'report:approve','reports','approve', 'Review and approve or reject submitted reports');

INSERT INTO roles (id, key, name, description, is_system) VALUES
('00000000-0000-4000-8000-000000000001', 'superadmin', 'Superadmin', 'Full platform access. Manages admins, roles and the access matrix.', true),
('00000000-0000-4000-8000-000000000002', 'admin',      'Admin',      'Manages users, events and reports. Cannot change the access matrix.', true),
('00000000-0000-4000-8000-000000000003', 'manager',    'Manager',    'Runs events end to end and writes reports.', true),
('00000000-0000-4000-8000-000000000004', 'staff',      'Staff',      'Supports event delivery and drafts reports.', true),
('00000000-0000-4000-8000-000000000005', 'member',     'Member',     'Platform member with no back-office access.', true);

-- superadmin: every permission
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p WHERE r.key = 'superadmin';

-- admin: everything except role:write
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p ON p.key IN (
    'user:read', 'user:write', 'user:delete', 'role:read',
    'event:read', 'event:write', 'event:delete', 'event:publish',
    'report:read', 'report:write', 'report:delete', 'report:approve'
) WHERE r.key = 'admin';

-- manager: event operations and report writing
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p ON p.key IN (
    'event:read', 'event:write', 'event:delete', 'event:publish',
    'report:read', 'report:write'
) WHERE r.key = 'manager';

-- staff: read events, write reports
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r JOIN permissions p ON p.key IN (
    'event:read', 'report:read', 'report:write'
) WHERE r.key = 'staff';

-- member has no back-office permissions by design
