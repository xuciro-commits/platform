import { readFileSync } from 'node:fs'

const pair = (car, cdr) => ({ car, cdr })
const list = (...items) => items.reduceRight((rest, item) => pair(item, rest), null)
const isPair = (value) => value !== null && typeof value === 'object'
const isSymbol = (value) => typeof value === 'string'
const second = (items) => items.cdr.car
const third = (items) => items.cdr.cdr.car
const fourth = (items) => items.cdr.cdr.cdr.car

function read(text) {
  const tokens = text.match(/[()']|[^\s()']+/g) ?? []
  let at = 0
  const next = () => {
    const token = tokens[at++]
    if (token === '(') {
      const items = []
      while (tokens[at] !== ')') items.push(next())
      at++
      return list(...items)
    }
    if (token === "'") return list('quote', next())
    if (token === '#t') return true
    if (token === '#f') return false
    return /^-?\d+$/.test(token) ? BigInt(token) : token
  }
  const forms = []
  while (at < tokens.length) forms.push(next())
  return list(...forms)
}

const show = (value) =>
  value === null ? '()' : value === true ? '#t' : value === false ? '#f' : isPair(value) ? `(${showItems(value)})` : String(value)

const showItems = (items) =>
  show(items.car) + (items.cdr === null ? '' : isPair(items.cdr) ? ` ${showItems(items.cdr)}` : ` . ${show(items.cdr)}`)

function evaluate(expression, environment, program) {
  for (;;) {
    if (isSymbol(expression)) return lookup(expression, environment, program)
    if (!isPair(expression)) return expression
    const head = expression.car
    const rest = expression.cdr
    if (head === 'quote') return rest.car
    if (head === 'lambda') return list('closure', rest.car, second(rest), environment)
    if (head === 'cond') {
      let clauses = rest
      while (clauses.car.car !== 'else' && evaluate(clauses.car.car, environment, program) === false) clauses = clauses.cdr
      expression = second(clauses.car)
      continue
    }
    const procedure = evaluate(head, environment, program)
    const args = evaluateEach(rest, environment, program)
    if (procedure.car !== 'closure') return primitives[second(procedure)](args)
    expression = third(procedure)
    environment = bind(second(procedure), args, fourth(procedure))
  }
}

const evaluateEach = (expressions, environment, program) =>
  expressions === null
    ? null
    : pair(evaluate(expressions.car, environment, program), evaluateEach(expressions.cdr, environment, program))

function lookup(name, environment, program) {
  for (; environment !== null; environment = environment.cdr) {
    if (name === environment.car.car) return environment.car.cdr
  }
  for (; program !== null; program = program.cdr) {
    if (name === second(program.car).car) return list('closure', second(program.car).cdr, third(program.car), null)
  }
  return list('primitive', name)
}

const bind = (names, args, environment) =>
  names === null ? environment : pair(pair(names.car, args.car), bind(names.cdr, args.cdr, environment))

const primitives = {
  car: (args) => args.car.car,
  cdr: (args) => args.car.cdr,
  cons: (args) => pair(args.car, second(args)),
  list: (args) => args,
  'eq?': (args) => args.car === second(args),
  'null?': (args) => args.car === null,
  'pair?': (args) => isPair(args.car),
  'symbol?': (args) => isSymbol(args.car),
  '+': (args) => args.car + second(args),
  '-': (args) => args.car - second(args),
  '=': (args) => args.car === second(args),
  '<': (args) => args.car < second(args),
  quotient: (args) => args.car / second(args),
  remainder: (args) => args.car % second(args),
}

const program = read(readFileSync(new URL('./canon.scm', import.meta.url), 'utf8'))
const within = (expression) => list('evaluate', list('quote', expression), list('quote', null), list('quote', program))

let checked = 0
const failures = []
const check = (source, expected) => {
  const expression = read(source).car
  const depths = [expression, within(expression), within(within(expression))]
  depths.forEach((nested, depth) => {
    const got = show(evaluate(nested, null, program))
    checked++
    if (got !== expected) failures.push(`depth ${depth}: ${source}\n  expected ${expected}\n  got      ${got}`)
  })
}

check('(common-measure 1071 462)', '21')
check('(common-measure 462 1071)', '21')
check('(common-measure 17 0)', '17')
check('(common-measure 123456789012345678901234567890 987654321098765432109876543210)', '9000000000900000000090')

for (let high = 0; high <= 6; high++) {
  for (let first = 0; first <= high; first++) {
    check(`(bisect (lambda (n) (< ${first - 1} n)) 0 ${high})`, String(first))
  }
}
check('(bisect (lambda (n) (< 1999 (+ n n))) 0 1000000000000)', '1000')
check('(bisect (lambda (n) (< n 0)) 5 5)', '5')

const decide = `(lambda (state request)
  (cond ((= (car state) (car request)) (list 'accepted (second request)))
        (else (list 'refused))))`
const apply = `(lambda (state result)
  (cond ((eq? (car result) 'accepted) (list (+ (car state) 1) (+ (second state) (second result))))
        (else state)))`
const ledger = (body) => `((lambda (decide apply initial) ${body}) ${decide} ${apply} '(0 0))`
const once = `(accept decide apply initial '() 'first '(0 5))`
const again = `(accept decide apply initial ${once} 'first '(0 99))`
const stale = `(accept decide apply initial ${once} 'second '(0 7))`
const fresh = `(accept decide apply initial ${stale} 'third '(1 7))`

check(ledger(once), '((first accepted 5))')
check(ledger(again), '((first accepted 5))')
check(ledger(stale), '((second refused) (first accepted 5))')
check(ledger(fresh), '((third accepted 7) (second refused) (first accepted 5))')
check(ledger(`(replay apply initial '())`), '(0 0)')
check(ledger(`(replay apply initial ${stale})`), '(1 5)')
check(ledger(`(replay apply initial ${fresh})`), '(2 12)')
check(ledger(`(recorded 'second ${fresh})`), '(second refused)')
check(ledger(`(recorded 'absent ${fresh})`), '#f')

check('(((lambda (x) (lambda (y) (- x y))) 10) 3)', '7')
check('((lambda (x) ((lambda (x) (+ x 1)) (+ x x))) 5)', '11')
check('((lambda (combine) (combine 2 3)) +)', '5')
check("(cons 'a '(b c))", '(a b c)')

if (failures.length > 0) {
  console.error(failures.join('\n'))
  process.exit(1)
}
console.log(`${checked} checks hold: directly, through evaluate, and through evaluate evaluating evaluate`)
