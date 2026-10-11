(module
  (memory (export "memory") 8)
  ;; Input: ptr:f32[count], count:i32, threshold:f32, output ptr.
  ;; Output f32[5]: sum, mean, min, max, count_above_threshold.
  (func (export "stats") (param $ptr i32) (param $len i32) (param $threshold f32) (param $out i32)
    (local $i i32) (local $v f32) (local $sum f64) (local $min f32) (local $max f32) (local $gt i32)
    (local.set $min (f32.const 3.402823466e+38))
    (local.set $max (f32.const -3.402823466e+38))
    (block $done
      (loop $loop
        (br_if $done (i32.ge_u (local.get $i) (local.get $len)))
        (local.set $v (f32.load (i32.add (local.get $ptr) (i32.mul (local.get $i) (i32.const 4)))))
        (local.set $sum (f64.add (local.get $sum) (f64.promote_f32 (local.get $v))))
        (if (f32.lt (local.get $v) (local.get $min)) (then (local.set $min (local.get $v))))
        (if (f32.gt (local.get $v) (local.get $max)) (then (local.set $max (local.get $v))))
        (if (f32.gt (local.get $v) (local.get $threshold)) (then (local.set $gt (i32.add (local.get $gt) (i32.const 1)))))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $loop)
      )
    )
    (f32.store (local.get $out) (f32.demote_f64 (local.get $sum)))
    (f32.store (i32.add (local.get $out) (i32.const 4)) (if (result f32) (i32.eqz (local.get $len)) (then (f32.const 0)) (else (f32.demote_f64 (f64.div (local.get $sum) (f64.convert_i32_u (local.get $len)))))))
    (f32.store (i32.add (local.get $out) (i32.const 8)) (if (result f32) (i32.eqz (local.get $len)) (then (f32.const 0)) (else (local.get $min))))
    (f32.store (i32.add (local.get $out) (i32.const 12)) (if (result f32) (i32.eqz (local.get $len)) (then (f32.const 0)) (else (local.get $max))))
    (f32.store (i32.add (local.get $out) (i32.const 16)) (f32.convert_i32_u (local.get $gt)))
  )
)