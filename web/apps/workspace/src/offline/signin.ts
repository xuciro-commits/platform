import type {Api} from "@platform/kernel";
type PublicSignIn=Pick<Api.SignIn,"issuer"|"client">;
const key="prepared:sign-in";
export function savePreparedSignIn(value:Api.SignIn){
 localStorage.setItem(key,JSON.stringify({...(value.issuer?{issuer:value.issuer}:{}),...(value.client?{client:value.client}:{})}));
}
export function preparedSignIn():PublicSignIn|undefined{
 const raw=localStorage.getItem(key);if(raw===null)return;
 try{const value=JSON.parse(raw);if(!value||typeof value!=="object"||Array.isArray(value)||Object.keys(value).some(key=>key!=="issuer"&&key!=="client")||value.issuer!==undefined&&typeof value.issuer!=="string"||value.client!==undefined&&typeof value.client!=="string")throw Error();return value;}
 catch{throw Error("Prepared sign-in configuration could not be opened. Connect and reload.");}
}
