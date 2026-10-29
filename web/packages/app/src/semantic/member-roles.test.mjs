import assert from 'node:assert/strict';
import test from 'node:test';
import {memberRoles} from './member-roles.ts';

test('secondary grants open the same application without replacing the primary role',()=>{
 const member={roles:{build:'user'},grants:[{app:'build',role:'builder'},{app:'build',role:'user'},{app:'ai',role:'admin'}]};
 assert.deepEqual(memberRoles(member,'build'),['user','builder']);
 assert.equal(member.roles.build,'user');
 assert.deepEqual(memberRoles(member,'ai'),['admin']);
 assert.deepEqual(memberRoles(member,'core'),[]);
});
