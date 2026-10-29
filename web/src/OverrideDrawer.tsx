import { useEffect, useId, useMemo, useRef, useState } from "react";
import { applyProjectOverride, previewProjectOverride, type ManagedSourceInput, type OverridePreview, type ProjectConfiguration } from "./api";
import { ComposeSourceFields, FileUploadField } from "./NewProjectDrawer";
import { ConfirmationDialog } from "./ConfirmationDialog";
import app from "./App.module.css";
import form from "./NewProjectDrawer.module.css";
import styles from "./OverrideDrawer.module.css";

type DiffLine={text:string;change:"same"|"added"|"removed"|"modified"};
export function composeDiff(before:string,after:string):DiffLine[]{
 const a=before.split("\n"),b=after.split("\n");
 if(a.at(-1)==="")a.pop();if(b.at(-1)==="")b.pop();
 const output:DiffLine[]=[];
 // Bound diff work for very large sources. The fallback remains an exact
 // before/after representation rather than suppressing changed content.
 if((a.length+1)*(b.length+1)>4_000_000){
  let p=0;while(p<a.length&&p<b.length&&a[p]===b[p])output.push({text:a[p++],change:"same"});
  let x=a.length,y=b.length;while(x>p&&y>p&&a[x-1]===b[y-1]){x--;y--;}
  for(const text of a.slice(p,x))output.push({text,change:"removed"});for(const text of b.slice(p,y))output.push({text,change:"added"});for(const text of b.slice(y))output.push({text,change:"same"});
 }else{
  const width=b.length+1,table=new Uint32Array((a.length+1)*width);
  for(let i=a.length-1;i>=0;i--)for(let j=b.length-1;j>=0;j--)table[i*width+j]=a[i]===b[j]?1+table[(i+1)*width+j+1]:Math.max(table[(i+1)*width+j],table[i*width+j+1]);
  let i=0,j=0;while(i<a.length||j<b.length){
   if(i<a.length&&j<b.length&&a[i]===b[j]){output.push({text:a[i++],change:"same"});j++;}
   else if(i<a.length&&(j===b.length||table[(i+1)*width+j]>=table[i*width+j+1]))output.push({text:a[i++],change:"removed"});
   else output.push({text:b[j++],change:"added"});
  }
 }
 for(let i=0;i<output.length;i++)if(output[i].change==="removed"){let replaced=0;while(i<output.length&&output[i].change==="removed"){i++;replaced++;}while(replaced-->0&&i<output.length&&output[i].change==="added")output[i++].change="modified";i--;}
 return output;
}

export function OverrideDrawer({projectID,csrfToken,configuration,removePath,onClose,onSaved}:{projectID:string;csrfToken:string;configuration:ProjectConfiguration;removePath?:string;onClose:()=>void;onSaved:()=>Promise<unknown>}){
 const dialog=useRef<HTMLDialogElement>(null),invoker=useRef<HTMLElement|null>(null);
 const title=useId(),generation=useRef(0),alive=useRef(true);
 const [kind,setKind]=useState<ManagedSourceInput["kind"]>("url"),[url,setURL]=useState(""),[yaml,setYAML]=useState(""),[file,setFile]=useState<File|null>(null);
 const [variables,setVariables]=useState<{name:string;value:string}[]>([]),[files,setFiles]=useState<Record<string,string>>({}),[filePath,setFilePath]=useState("");
 const [preview,setPreview]=useState<OverridePreview|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(""),[discard,setDiscard]=useState(false),[showFinal,setShowFinal]=useState(false);
 const dirty=!!(url||yaml||file||preview||variables.length||Object.keys(files).length);
 const lines=useMemo(()=>preview?composeDiff(preview.before,preview.after):[],[preview]);
 useEffect(()=>{alive.current=true;invoker.current=document.activeElement instanceof HTMLElement?document.activeElement:null;dialog.current?.showModal();return()=>{alive.current=false;generation.current++;invoker.current?.focus();};},[]);
 function close(){if(busy)return;if(dirty&&!removePath)setDiscard(true);else onClose();}
 async function read(){
  const version=++generation.current;setBusy(true);setError("");setPreview(null);
  try{
   let source:ManagedSourceInput={kind:"paste",yaml:""};
   if(!removePath){if(kind==="url")source={kind,url:url.trim()};else if(kind==="paste")source={kind,yaml};else{if(!file)throw Error("Choose an override file.");if(file.size>1024*1024)throw Error("Compose file exceeds 1 MiB.");source={kind,filename:file.name,yaml:new TextDecoder("utf-8",{fatal:true}).decode(await file.arrayBuffer())};}}
   const variableValues:Record<string,string>={};for(const row of variables){if(!/^[A-Za-z_][A-Za-z0-9_]*$/.test(row.name)||row.name in variableValues)throw Error("Use distinct valid variable names.");variableValues[row.name]=row.value;}
   const result=await previewProjectOverride(projectID,{variables:variableValues,files,action:removePath?"remove":"add",path:removePath,source,fingerprint:configuration.fingerprint},csrfToken);
   if(alive.current&&version===generation.current)setPreview(result);
  }catch(cause){if(alive.current&&version===generation.current)setError(cause instanceof Error?cause.message:"Unable to review override.");}
  finally{if(alive.current&&version===generation.current)setBusy(false);}
 }
 useEffect(()=>{if(removePath)void read();},[]);
 async function save(){if(!preview||busy)return;const version=++generation.current;setBusy(true);setError("");try{await applyProjectOverride(projectID,preview,csrfToken);if(!alive.current||version!==generation.current)return;await onSaved();onClose();}catch(cause){if(alive.current)setError(cause instanceof Error?cause.message:"Unable to save override.");}finally{if(alive.current)setBusy(false);}}
 return <><dialog ref={dialog} className={`${app.settingsDrawer} ${app.projectDrawer}`} aria-labelledby={title} onClose={event=>event.stopPropagation()} onCancel={event=>{event.stopPropagation();event.preventDefault();close();}} onClick={event=>{if(event.target===dialog.current)close();}}>
  <div className={app.settingsBody}><header className={app.settingsHeader}><div><span className={app.sectionLabel}>COMPOSE CONFIGURATION</span><h2 id={title}>{removePath?"Remove override":"Add override"}</h2></div><button type="button" className={app.closeButton} autoFocus disabled={busy} onClick={close} aria-label="Close override"><i className="ph-bold ph-x" aria-hidden="true" /></button></header>
  <div className={`${app.settingsContent} ${form.content}`}>
   {!preview&&!removePath&&<form className={form.form} onSubmit={event=>{event.preventDefault();void read();}}><p>The original Compose file stays unchanged. Review the combined configuration before saving.</p><ComposeSourceFields prefix="override" kind={kind} url={url} yaml={yaml} file={file} busy={busy} onKind={setKind} onURL={setURL} onYAML={setYAML} onFile={setFile}/><details><summary>Interpolation values and project dependencies</summary><fieldset disabled={busy}><p>Optional values are not required. Add any required variables or local files reported by Compose. Existing files are kept.</p>{variables.map((row,index)=><div className={form.envRow} key={index}><label>Variable<input value={row.name} onChange={event=>setVariables(current=>current.map((value,i)=>i===index?{...value,name:event.target.value}:value))} /></label><label>Value<input type="password" autoComplete="off" value={row.value} onChange={event=>setVariables(current=>current.map((value,i)=>i===index?{...value,value:event.target.value}:value))} /></label><button type="button" onClick={()=>setVariables(current=>current.filter((_,i)=>i!==index))}>Remove variable</button></div>)}<button type="button" onClick={()=>setVariables(current=>[...current,{name:"",value:""}])}>Add variable</button><div className={form.field}><label htmlFor={`${title}-dependency`}>Dependency path relative to the project</label><input id={`${title}-dependency`} value={filePath} placeholder="config/settings.yaml" onChange={event=>setFilePath(event.target.value)} /></div><FileUploadField id={`${title}-file`} label="Add dependency file" accept=".env,.txt,.yaml,.yml,.json,text/plain" hint="UTF-8 · Up to 1 MiB per file" disabled={busy} onChange={event=>{const next=event.target.files?.[0];if(!next)return;const name=filePath.trim()||next.name,version=generation.current;if(next.size>1024*1024){setError("Project dependency exceeds 1 MiB.");return;}void next.arrayBuffer().then(value=>{if(alive.current&&version===generation.current)setFiles(current=>({...current,[name]:new TextDecoder("utf-8",{fatal:true}).decode(value)}));}).catch(()=>{if(alive.current&&version===generation.current)setError("Project files must contain UTF-8 text.");});event.target.value="";}} />{Object.keys(files).map(name=><p key={name}><code>{name}</code> <button type="button" onClick={()=>setFiles(current=>{const next={...current};delete next[name];return next;})}>Remove file</button></p>)}</fieldset></details><button type="submit" className={app.primaryButton} disabled={busy}>{busy?"Reading…":"Read source"}</button></form>}
   {preview&&<section className={styles.review} aria-label="Review effective Compose"><p>{removePath?"Remove from selection (host file will be kept):":"Save separate override:"} <code>{preview.path}</code></p><p>Compose order: {preview.composeFiles.join(" → ")}</p><p>Saving does not change containers. Use Recreate afterwards.</p><button type="button" className={app.inspectButton} onClick={()=>setShowFinal(value=>!value)}>{showFinal?"Show changes":"Show final Compose"}</button>
   <p className={styles.legend}>+ Added · ~ Changed · − Removed</p><pre className={styles.diff} data-drawer-swipe-ignore aria-label={showFinal?"Final effective Compose":"Compose changes"}>{showFinal?preview.after:lines.map((line,i)=><span key={i} data-change={line.change}><span aria-hidden="true">{({same:" ",added:"+",removed:"−",modified:"~"})[line.change]} </span>{line.text}{"\n"}</span>)}</pre>
   <div className={styles.actions}><button type="button" className={app.inspectButton} disabled={busy} onClick={onClose}>Cancel</button>{!removePath&&<button type="button" className={app.inspectButton} disabled={busy} onClick={()=>setPreview(null)}>Back</button>}<button type="button" className={app.primaryButton} disabled={busy} onClick={()=>{void save();}}>{busy?"Saving…":removePath?"Remove from selection":"Add override"}</button></div></section>}
   {error&&<p className={app.formError} role="alert">{error}</p>}
  </div></div>
 </dialog><ConfirmationDialog open={discard} title="Discard override draft?" onClose={()=>setDiscard(false)} actions={<><button type="button" className={app.inspectButton} autoFocus onClick={()=>setDiscard(false)}>Keep editing</button><button type="button" className={app.inspectButton} onClick={onClose}>Discard</button></>}><p>The override has not been saved.</p></ConfirmationDialog></>;
}
