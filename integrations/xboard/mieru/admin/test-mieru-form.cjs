const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const source = fs.readFileSync(__dirname + '/mieru-form.js', 'utf8');
const jsx = (type, props) => ({type, props});
const names = ['$y','Gy','Zy','Yy','fzt','bzt','vzt','wzt','_zt','Szt','u8e'];
let state = false;
const context = {Q: {jsx, jsxs: jsx, Fragment: 'fragment'}, H: {useState: () => [state, x => state = x], useId: () => 'pattern-input'}, J4t: {transportOptions:[{value:'TCP',label:'TCP'},{value:'UDP',label:'UDP'}]}};
for (const name of names) context[name] = name;
const component = vm.runInNewContext('(' + source + ')', context);
function render(value, transport='TCP') {
  let current = value;
  const form = {control:{}, watch:name => name === 'transport' ? transport : current};
  const tree = component({form,t:x=>x});
  const field = {value:current, name:'traffic_pattern', onChange:x=>{current=typeof x==='string'?x:x.target.value}, onBlur(){}};
  const content = tree.props.children[1].props.render({field});
  const select = content.props.children[1].props.children;
  return {select, content, value:()=>current};
}
for (const [value, mode] of [['','default'],['CI3R1AkQABoECAAQACICCAA=','baseline'],['CI3R1AkQABoECAEQACICCAA=','fragment'],['existing-custom','custom']]) {
  state=false; assert.equal(render(value).select.props.value,mode);
}
state=false;
let view=render('existing-custom');
view.select.props.onChange({target:{value:'custom'}});
assert.equal(view.value(),'existing-custom');
view.select.props.onChange({target:{value:'fragment'}});
assert.equal(view.value(),'CI3R1AkQABoECAEQACICCAA=');
view.select.props.onChange({target:{value:'baseline'}});
assert.equal(view.value(),'CI3R1AkQABoECAAQACICCAA=');
view.select.props.onChange({target:{value:'default'}});
assert.equal(view.value(),'');
state=false;
assert.equal(render('', 'UDP').select.props.children[2].props.disabled,true);
assert.equal(render('', 'TCP').select.props.children[2].props.disabled,false);
console.log('PASS: preset selection, reload mapping, custom preservation, TCP/UDP gating');
