// One draft for game.yaml and fingerprints.yaml; the server validates both.
const catalogRegister={base:null,revision:"",raw:null,releases:[],fingerprints:[],defaultRelease:"",pages:{},open:{},query:"",loaded:false};
const registerClone=value=>JSON.parse(JSON.stringify(value));
function catalogRegisterInstall(snapshot,baseline=false){
 const r=catalogRegister;
 if(baseline){r.base=snapshot?registerClone(snapshot):null;r.revision=snapshot?.revision||"";r.raw=null;r.query="";r.pages={};r.open={};}
 r.loaded=!!snapshot;
 r.releases=(snapshot?.releases||[]).map(x=>({original:x.name,name:x.name,fields:registerClone(x.fields),base:registerClone(x.fields)}));
 r.fingerprints=(snapshot?.fingerprints||[]).map(x=>({index:x.index,fields:registerClone(x.fields),base:registerClone(x.fields)}));
 r.defaultRelease=snapshot?.default_release||"";r.snapshot=snapshot;
}
function registerDiff(base,fields){
 const patch={};
 for(const key of new Set([...Object.keys(base),...Object.keys(fields)])){
  if(catalogEqualValue(base[key],fields[key]))continue;
  if(key==="file"&&base[key]&&fields[key])patch[key]=registerDiff(base[key],fields[key]);
  else patch[key]=fields[key]??null;
 }
 return patch;
}
function catalogRegisterPatch(){
 const r=catalogRegister,patch={};
 const releases=r.releases.filter(x=>x.original||!x.deleted).map(x=>({original:x.original,name:x.name,fields:registerDiff(x.base,x.fields),...(x.deleted?{delete:true,...(x.replacement?{replacement:x.replacement}:{})}:{})})).filter(x=>x.delete||!x.original||x.name!==x.original||Object.keys(x.fields).length);
 const fingerprints=r.fingerprints.filter(x=>x.index>=0||!x.deleted).map(x=>({index:x.index,fields:registerDiff(x.base,x.fields),...(x.deleted?{delete:true}:{})})).filter(x=>x.delete||x.index<0||Object.keys(x.fields).length);
 if(releases.length)patch.releases=releases;
 if(fingerprints.length)patch.fingerprints=fingerprints;
 if(r.defaultRelease!==(r.snapshot?.default_release||""))patch.default_release=r.defaultRelease;
 return patch;
}
function catalogRegisterDirty(){return catalogRegister.loaded&&(catalogRegister.raw!==null||Object.keys(catalogRegisterPatch()).length>0);}
function catalogRegisterPayload(){
 const r=catalogRegister;return r.loaded?{register:{revision:r.revision,...(r.raw!==null?{yaml:r.raw}:{}),patch:catalogRegisterPatch()}}:{};
}
function catalogRegisterChanged(){
 const f=catalogInline;f.dirty=f.raw!==null||f.edits.size>0||catalogRegisterDirty();f.error="";f.message="";catalogEditorActions();
}
async function catalogEnsureRegister(){
 const f=catalogInline,r=catalogRegister;if(r.loaded)return true;if(f.saving||f.loading)return false;
 const version=f.version;f.saving=true;catalogInlineRender();
 try{
  const card=await fetchJSON("/api/catalog/edit?"+new URLSearchParams({platform:f.platform,game:f.folder,register:"1"}));
  if(version!==f.version||!catalogInlineCurrent())return false;
  if(card.revision!==f.baseRevision)throw new Error("Card changed since it was opened. Discard and reopen it before editing releases.");
  if(!card.register)throw new Error("Release editing is unavailable on this server.");
  catalogRegisterInstall(card.register,true);
 }catch(error){if(version===f.version){f.error=error.message;catalogEditorActions();}return false;}
 finally{if(version===f.version){f.saving=false;catalogInlineRender();}}
 if(f.dirty)return catalogMaterializeDraft();
 return true;
}
function catalogRegisterPreview(card,dirty){
 if(!card.register||!catalogRegister.loaded)return;
 if(dirty){catalogRegister.raw=card.register.yaml;catalogRegisterInstall(card.register);}
}
function catalogRegisterAdd(kind,owner){
 if(catalogInline.saving)return;
 const r=catalogRegister;
 if(kind==="release")r.releases.push({original:"",name:"",fields:{},base:{}});
 else {const target=owner??("release:"+ (r.releases.find(x=>!x.deleted)?.name||""));const [type,...name]=target.split(":");const fields={file:{name:"",size:0}};if(type==="release"||type==="variant")fields[type]=name.join(":");r.fingerprints.push({index:-1,fields,base:{}});r.query="";r.open[r.fingerprints.length-1]=true;r.pages[target]=Math.floor((r.fingerprints.filter(x=>registerOwner(x)===target).length-1)/20);}
 catalogRegisterChanged();catalogInlineRender();
 const selector=kind==="release"?'[data-register-field="name"]':'[data-register-field="file.name"]';
 const index=kind==="release"?r.releases.length-1:r.fingerprints.length-1;document.querySelector?.('[data-register-record="'+kind+'-'+index+'"] '+selector)?.focus?.();
}
function catalogRegisterDelete(kind,index){
 if(catalogInline.saving)return;
 const entry=(kind==="release"?catalogRegister.releases:catalogRegister.fingerprints)[index];
 entry.deleted=!entry.deleted;if(kind==="fingerprint"&&entry.deleted)catalogInline.invalid.delete("register:"+index+":size");catalogRegisterChanged();catalogInlineRender();
}
function catalogRegisterChange(kind,index,key,text){
 if(catalogInline.saving)return;
 const r=catalogRegister,entry=(kind==="release"?r.releases:r.fingerprints)[index];
 if(key==="replacement"){entry.replacement=text.replace(/^release:/,"");}
 else if(key==="name"){
  const previous=entry.name;entry.name=text;
  if(previous){
   if(previous&&r.defaultRelease===previous)r.defaultRelease=text;
   r.fingerprints.forEach(fp=>{if(fp.fields.release===previous)fp.fields.release=text;});
  }
 }else if(key==="target"){
  delete entry.fields.release;delete entry.fields.variant;
  const [type,...name]=text.split(":");if(type==="release"||type==="variant")entry.fields[type]=name.join(":");
 }else {
  let value=text;
  if(["regions","languages"].includes(key))value=text.split(",").map(v=>v.trim()).filter(Boolean);
  if(key==="file.size"){value=text===""?null:Number(text);if(value!==null&&(!Number.isSafeInteger(value)||value<0)){catalogInline.invalid.set("register:"+index+":size",text);catalogInline.error="File size must be a nonnegative whole number of bytes.";catalogEditorActions();return;}catalogInline.invalid.delete("register:"+index+":size");}
  if(key.startsWith("file.")){
   entry.fields.file={...(entry.fields.file||{})};const field=key.slice(5);
   if(value===""||value===null)delete entry.fields.file[field];else entry.fields.file[field]=value;
  }else if(value==="")delete entry.fields[key];else entry.fields[key]=value;
 }
 catalogRegisterChanged();
}
function registerInput(kind,index,key,label,value,type="text"){
 const text=key==="file.size"&&catalogInline.invalid.has("register:"+index+":size")?catalogInline.invalid.get("register:"+index+":size"):Array.isArray(value)?value.join(", "):value??"";
 return '<label><span>'+cEscape(label)+'</span><input type="'+type+'" data-register-kind="'+kind+'" data-register-index="'+index+'" data-register-field="'+key+'" aria-label="'+escapeAttr(label)+'" value="'+escapeAttr(text)+'"'+(key==="file.size"?' min="0" step="1"':'')+(catalogInline.saving?' disabled':'')+'></label>';
}
function registerOptions(value,variant=false){
 const r=catalogRegister,options=[...r.releases.filter(x=>!x.deleted&&x.name).map(x=>["release:"+x.name,x.name]),...(variant?(r.snapshot?.variants||[]).map(x=>["variant:"+x.name,"Variant · "+x.name]):[])];
 return '<option value="">Choose a release'+(variant?' or variant':'')+'</option>'+(!options.some(([id])=>id===value)&&value?'<option value="'+escapeAttr(value)+'" selected>Missing · '+cEscape(value.replace(/^(release|variant):/,""))+'</option>':'')+options.map(([id,name])=>'<option value="'+escapeAttr(id)+'" '+(id===value?'selected':'')+'>'+cEscape(name)+'</option>').join("");
}
function registerOwner(fp){return fp.fields.variant?"variant:"+fp.fields.variant:fp.fields.release?"release:"+fp.fields.release:"unassigned";}
function registerRemove(kind,index,deleted){
 return '<button type="button" class="tab-button '+(deleted?'':'register-delete')+'" data-register-delete="'+kind+'" data-register-index="'+index+'" '+(catalogInline.saving?'disabled':'')+'>'+(deleted?'Undo removal':'Remove')+'</button>';
}
function registerFiles(owner){
 const r=catalogRegister,all=r.fingerprints.map((x,i)=>({x,i})).filter(({x})=>registerOwner(x)===owner),filtered=all.filter(({x})=>!r.query||JSON.stringify(x.fields).toLowerCase().includes(r.query.toLowerCase()));
 const page=r.pages[owner]=Math.max(0,Math.min(r.pages[owner]||0,Math.ceil(filtered.length/20)-1));
 return '<div class="register-files"><div class="register-heading"><h4>Files / fingerprints <span>'+all.filter(({x})=>!x.deleted).length+'</span></h4>'+
 (owner!=="unassigned"?'<button type="button" class="tab-button" data-register-add="fingerprint" data-register-owner="'+escapeAttr(owner)+'" '+(owner==="release:"?'disabled title="Name the release first"':'')+'>+ Add file</button>':'')+'</div>'+
 (!filtered.length?'<p class="secondary">'+(r.query?'No matching files.':'No registered files yet.')+'</p>':'')+
 filtered.slice(page*20,page*20+20).map(({x,i})=>'<details class="register-file" data-register-open="'+i+'" '+(r.open[i]?'open':'')+'><summary><span>'+cEscape(x.fields.file?.name||"New fingerprint")+'</span><small>'+ (x.deleted?'Removal pending':cEscape(x.fields.file?.size??"Unknown size")+" bytes")+'</small></summary><fieldset class="register-record" data-register-record="fingerprint-'+i+'" '+(catalogInline.saving?'disabled':'')+'><div class="register-heading"><strong>File identity</strong>'+registerRemove("fingerprint",i,x.deleted)+'</div>'+
 (x.deleted?'<p class="register-removal">Will be removed on Save. The ROM file is not deleted.</p>':'<div class="register-fields">'+registerInput("fingerprint",i,"file.name","File name",x.fields.file?.name)+
 '<label><span>Move to release or variant</span><select aria-label="Move to release or variant" data-register-kind="fingerprint" data-register-index="'+i+'" data-register-field="target">'+registerOptions((x.fields.variant?'variant:'+x.fields.variant:x.fields.release?'release:'+x.fields.release:''),true)+'</select></label>'+
 registerInput("fingerprint",i,"file.size","Size in bytes",x.fields.file?.size,"number")+registerInput("fingerprint",i,"file.crc32","CRC32 · 8 hex characters",x.fields.file?.crc32)+registerInput("fingerprint",i,"file.md5","MD5 · 32 hex characters",x.fields.file?.md5)+registerInput("fingerprint",i,"file.sha1","SHA1 · 40 hex characters",x.fields.file?.sha1)+registerInput("fingerprint",i,"file.sha256","SHA256 · 64 hex characters",x.fields.file?.sha256)+'</div>'+
 (x.fields.file?.kind?'<p class="secondary">Identity kind: '+cEscape(x.fields.file.kind)+'</p>':'')+'<p class="secondary register-hint">At least one hash is required. Source provenance and additional fields are retained.</p>')+'</fieldset></details>').join("")+
 (filtered.length>20?'<div class="register-heading register-pages"><button type="button" class="tab-button" data-register-page="-1" data-register-owner="'+escapeAttr(owner)+'" '+(!page?'disabled':'')+'>Previous</button><span>Page '+(page+1)+' of '+Math.ceil(filtered.length/20)+'</span><button type="button" class="tab-button" data-register-page="1" data-register-owner="'+escapeAttr(owner)+'" '+((page+1)*20>=filtered.length?'disabled':'')+'>Next</button></div>':'')+'</div>';
}
function catalogRenderRegister(){
 const r=catalogRegister;
 if(!r.loaded)return '<p class="secondary">Open the release editor to load these records.</p><button class="tab-button" id="register-load">Load releases & fingerprints</button>';
 const owners=new Set(r.releases.map(x=>"release:"+x.name));
 const extra=[...new Set(r.fingerprints.map(registerOwner))].filter(owner=>!owners.has(owner));
 return '<div class="register-editor"><div class="register-heading"><p class="secondary">Each release contains its registered files. Changes are saved together; ROM files stay unchanged.</p><button class="tab-button" id="register-validate" '+(catalogInline.saving?'disabled':'')+'>Validate changes</button></div>'+
 '<div class="register-heading"><h3>Releases <span>'+r.releases.filter(x=>!x.deleted).length+'</span></h3><button type="button" class="tab-button" data-register-add="release">+ Add release</button></div>'+
 '<div class="register-fields"><label class="register-default">Default release<select id="register-default" aria-label="Default release" '+(catalogInline.saving?'disabled':'')+'>'+registerOptions(r.defaultRelease?'release:'+r.defaultRelease:'')+'</select></label><label class="register-search">Find a file or hash<input id="register-search" aria-label="Find a file or hash" value="'+escapeAttr(r.query)+'" type="search"></label></div>'+
 '<div class="register-release-list">'+r.releases.map((x,i)=>'<section class="register-record register-release" data-register-record="release-'+i+'"><div class="register-heading"><h3>'+cEscape(x.name||"New release")+'</h3>'+registerRemove("release",i,x.deleted)+'</div>'+
 (x.deleted?'<p class="register-removal">Release removal pending. Remove its files below or reassign its references before saving.</p><label>Reassign its references to<select aria-label="Reassign its references to" data-register-kind="release" data-register-index="'+i+'" data-register-field="replacement">'+registerOptions(x.replacement?'release:'+x.replacement:'')+'</select></label>':'<div class="register-fields">'+registerInput("release",i,"name","Release name",x.name)+registerInput("release",i,"title","Display title",x.fields.title)+registerInput("release",i,"regions","Regions · comma separated",x.fields.regions)+registerInput("release",i,"languages","Languages · comma separated",x.fields.languages)+registerInput("release",i,"release_date","Release date",x.fields.release_date)+'</div>')+
 registerFiles("release:"+x.name)+'</section>').join("")+
 extra.map(owner=>'<section class="register-record"><h3>'+cEscape(owner.startsWith("variant:")?"Variant · "+owner.slice(8):owner==="unassigned"?"Unassigned files":"Missing release · "+owner.slice(8))+'</h3><p class="secondary">'+(owner.startsWith("variant:")?'These files belong to a derived variant, not to an official release.':'Assign these files to a release or variant before saving.')+'</p>'+registerFiles(owner)+'</section>').join("")+'</div></div>';
}
function catalogBindRegister(){
 if(state.detailTab!=="files")return;
 $("register-load")?.addEventListener("click",async()=>{await catalogEnsureRegister();catalogInlineRender();});
 $("register-validate")?.addEventListener("click",catalogMaterializeDraft);
 document.querySelectorAll("[data-register-add]").forEach(el=>el.addEventListener("click",()=>catalogRegisterAdd(el.dataset.registerAdd,el.dataset.registerOwner)));
 document.querySelectorAll("[data-register-delete]").forEach(el=>el.addEventListener("click",()=>catalogRegisterDelete(el.dataset.registerDelete,Number(el.dataset.registerIndex))));
 document.querySelectorAll("[data-register-open]").forEach(el=>el.addEventListener("toggle",()=>{catalogRegister.open[el.dataset.registerOpen]=el.open;}));
 document.querySelectorAll("[data-register-field]").forEach(el=>{
  el.addEventListener(el.tagName==="SELECT"?"change":"input",()=>catalogRegisterChange(el.dataset.registerKind,Number(el.dataset.registerIndex),el.dataset.registerField,el.value));
  if(["name","target"].includes(el.dataset.registerField))el.addEventListener("change",catalogInlineRender);
 });
 $("register-default")?.addEventListener("change",e=>{catalogRegister.defaultRelease=e.target.value.replace(/^release:/,"");catalogRegisterChanged();});
 $("register-search")?.addEventListener("input",e=>{catalogRegister.query=e.target.value;catalogRegister.pages={};catalogInlineRender();$("register-search")?.focus?.();});
 document.querySelectorAll("[data-register-page]").forEach(el=>el.addEventListener("click",()=>{catalogRegister.pages[el.dataset.registerOwner]+=Number(el.dataset.registerPage);catalogInlineRender();}));
}
