// Original workshop model generator; regenerate binary bytes from the source geometry.
import fs from "node:fs";
import {fileURLToPath} from "node:url";
import { BoxGeometry, CylinderGeometry, SphereGeometry } from "../../packages/ui/node_modules/three/build/three.module.js";

// Build a real, self-contained glTF 2.0 GLB with named, hierarchically articulated joints.
// No external textures/files; GLTFLoader can load it via loadAsync() and traverse the nodes.
const geometries = [new BoxGeometry(1, 1, 1), new CylinderGeometry(.5, .5, 1, 24), new SphereGeometry(.5, 20, 12)];
const bin = [];
let length = 0;
const bufferViews = [], accessors = [], meshes = [];
function write(typed, target) {
  const pad = (4 - length % 4) % 4;
  if (pad) { bin.push(Buffer.alloc(pad)); length += pad; }
  const bytes = Buffer.from(typed.buffer, typed.byteOffset, typed.byteLength);
  const idx = bufferViews.length;
  bufferViews.push({ buffer: 0, byteOffset: length, byteLength: bytes.length, target });
  bin.push(bytes); length += bytes.length;
  return idx;
}
for (const geo of geometries) {
  geo.computeBoundingBox();
  const pos = geo.getAttribute("position"), norm = geo.getAttribute("normal");
  const pi = write(pos.array, 34962), ni = write(norm.array, 34962);
  const idx = geo.index;
  const id = write(idx.array, 34963);
  const pAcc = accessors.length;
  accessors.push({ bufferView: pi, componentType: 5126, count: pos.count, type: "VEC3", min: geo.boundingBox.min.toArray(), max: geo.boundingBox.max.toArray() });
  const nAcc = accessors.length;
  accessors.push({ bufferView: ni, componentType: 5126, count: norm.count, type: "VEC3" });
  const iAcc = accessors.length;
  accessors.push({ bufferView: id, componentType: idx.array instanceof Uint32Array ? 5125 : 5123, count: idx.count, type: "SCALAR" });
  meshes.push({ primitives: [{ attributes: { POSITION: pAcc, NORMAL: nAcc }, indices: iAcc, material: 0 }] });
}
const materials = [
  ["IndustrialSteel", [0.31, .38, .46, 1], .62, .3],
  ["SafetyOrange", [1, .39, .06, 1], .25, .34],
  ["Graphite", [.13, .18, .24, 1], .45, .4],
  ["SensorBlue", [.07, .59, .9, 1], .16, .27],
  ["StatusGreen", [.12, .76, .48, 1], .15, .3],
  ["CautionYellow", [.96, .72, .16, 1], .2, .4],
].map(([name, baseColorFactor, metallicFactor, roughnessFactor]) => ({ name, pbrMetallicRoughness: { baseColorFactor, metallicFactor, roughnessFactor }, doubleSided: true }));
const nodes = [];
function group(name, translation = [0, 0, 0], children = []) {
  const i = nodes.length;
  nodes.push({ name, translation, children, extras: { logicalPart: name, telemetryBindable: true } });
  return i;
}
function part(name, primitive, material, translation, scale = [1, 1, 1]) {
  const mesh = meshes.length;
  meshes.push({ name: `${name}_mesh`, primitives: [{ ...meshes[primitive].primitives[0], material }] });
  const i = nodes.length;
  nodes.push({ name, mesh, translation, scale, extras: { logicalPart: name, ontologyObjectType: "Asset" } });
  return i;
}
const root = group("RobotCell");
const platform = part("Foundation", 0, 2, [0, .02, 0], [2.3, .08, 1.65]);
const base = part("BasePedestal", 1, 0, [0, .20, 0], [.68, .38, .68]);
const yaw = group("BaseYaw", [0, .42, 0]);
const yawMotor = part("YawMotor", 1, 2, [0, .08, 0], [.52, .24, .52]);
const turret = part("Turret", 0, 1, [0, .26, 0], [.45, .42, .42]);
const shoulder = group("ShoulderPitch", [0, .50, 0]);
const shoulderHousing = part("ShoulderHousing", 1, 0, [0, 0, 0], [.4, .22, .4]);
const upperArm = part("UpperArm", 0, 1, [0, .35, 0], [.29, .73, .3]);
const vibProbe = part("VibrationProbe", 2, 3, [.20, .52, 0], [.15, .15, .15]);
const elbow = group("ElbowPitch", [0, .77, 0]);
const elbowJoint = part("ElbowHousing", 1, 2, [0, 0, 0], [.39, .21, .39]);
const forearm = part("Forearm", 0, 0, [0, .31, 0], [.24, .62, .26]);
const tempProbe = part("TemperatureProbe", 2, 3, [-.17, .39, 0], [.11, .11, .11]);
const wrist = group("WristRoll", [0, .63, 0]);
const wristJoint = part("WristHousing", 1, 2, [0, .05, 0], [.30, .24, .30]);
const gripper = part("GripperBody", 0, 1, [0, .23, 0], [.32, .25, .27]);
const fingerA = group("FingerLeft", [-.17, .37, 0]);
const fingerB = group("FingerRight", [.17, .37, 0]);
const jawA = part("LeftJaw", 0, 0, [0, .10, 0], [.09, .22, .14]);
const jawB = part("RightJaw", 0, 0, [0, .10, 0], [.09, .22, .14]);
const pressureProbe = part("PressureProbe", 2, 4, [0, .44, -.08], [.12, .12, .12]);
const conveyor = group("ConveyorLayer", [1.15, 0, -.17]);
const conveyorBody = part("ConveyorBed", 0, 0, [0, .27, 0], [1.75, .16, .7]);
const belt = part("ConveyorBelt", 0, 2, [0, .365, 0], [1.7, .04, .65]);
const motor = part("ConveyorMotor", 1, 5, [.79, .28, 0], [.28, .30, .28]);
const sensor = part("ConveyorSensor", 2, 3, [-.50, .48, 0], [.12, .12, .12]);
const warning = part("SafetyBeacon", 2, 5, [.75, .7, -.56], [.14, .14, .14]);
// glTF child trees; joint rotations in the viewer animate descendants.
nodes[root].children = [platform, base, yaw, conveyor, warning];
nodes[yaw].children = [yawMotor, turret, shoulder];
nodes[shoulder].children = [shoulderHousing, upperArm, vibProbe, elbow];
nodes[elbow].children = [elbowJoint, forearm, tempProbe, wrist];
nodes[wrist].children = [wristJoint, gripper, fingerA, fingerB, pressureProbe];
nodes[fingerA].children = [jawA]; nodes[fingerB].children = [jawB];
nodes[conveyor].children = [conveyorBody, belt, motor, sensor];
const json = { asset: { version: "2.0", generator: "Workshop industrial digital twin demo" }, scene: 0, scenes: [{ name: "Robot work cell", nodes: [root] }], nodes, meshes, materials, buffers: [{ byteLength: length }], bufferViews, accessors };
const jsonBuffer = Buffer.from(JSON.stringify(json));
const jp = Buffer.concat([jsonBuffer, Buffer.alloc((4 - jsonBuffer.length % 4) % 4, 0x20)]);
const bp = Buffer.concat([...bin, Buffer.alloc((4 - length % 4) % 4)]);
const glb = Buffer.alloc(12 + 8 + jp.length + 8 + bp.length);
let off = 0;
glb.writeUInt32LE(0x46546c67, off); off += 4; glb.writeUInt32LE(2, off); off += 4; glb.writeUInt32LE(glb.length, off); off += 4;
glb.writeUInt32LE(jp.length, off); off += 4; glb.writeUInt32LE(0x4e4f534a, off); off += 4; jp.copy(glb, off); off += jp.length;
glb.writeUInt32LE(bp.length, off); off += 4; glb.writeUInt32LE(0x004e4942, off); off += 4; bp.copy(glb, off);
const dest = fileURLToPath(new URL("./industrial-robot-cell.glb",import.meta.url));
fs.writeFileSync(dest, glb);
console.log(`Generated ${dest}: ${(glb.length / 1024).toFixed(1)} KiB, ${nodes.length} named parts, ${materials.length} materials`);
