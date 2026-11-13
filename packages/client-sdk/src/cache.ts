import Dexie,{type Table} from 'dexie';
import type {Identity,Device} from '@reign/protocol';
export interface SavedDevice extends Device {key:string;service:string;expires:number;identityId:string}
export class ReignCache extends Dexie {
 identities!:Table<Identity,string>;devices!:Table<SavedDevice,string>;snapshots!:Table<{key:string;value:unknown;updated:number},string>;attachments!:Table<{id:string;blob:Blob;used:number},string>;
 constructor(){super('reign');this.version(1).stores({identities:'service',devices:'[service+id],service',snapshots:'key,updated',attachments:'id,used'});}
 async putSnapshot(key:string,value:unknown){await this.snapshots.put({key,value,updated:Date.now()});}
 async putAttachment(id:string,blob:Blob){if(blob.size>32*1024*1024)return;await this.attachments.put({id,blob,used:Date.now()});let rows=await this.attachments.orderBy('used').toArray();let size=rows.reduce((n,r)=>n+r.blob.size,0);for(const row of rows){if(size<=100*1024*1024)break;await this.attachments.delete(row.id);size-=row.blob.size;}}
}
