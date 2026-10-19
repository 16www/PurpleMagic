import test from 'node:test';
import assert from 'node:assert/strict';
import {numberDraft,draftNumber} from '../src/numeric.ts';
test('numeric drafts can be cleared and retyped without silently submitting zero',()=>{
 assert.equal(numberDraft(0),'0');assert.equal(numberDraft(NaN),'');
 assert.ok(Number.isNaN(draftNumber('')));assert.equal(draftNumber('65535'),65535);assert.equal(draftNumber('0'),0);
});
