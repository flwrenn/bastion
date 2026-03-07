// SPDX-License-Identifier: UNLICENSED
pragma solidity ^0.8.28;

import {Test} from "forge-std/Test.sol";
import {EntryPoint} from "@account-abstraction/contracts/core/EntryPoint.sol";
import {IEntryPoint} from "@account-abstraction/contracts/interfaces/IEntryPoint.sol";
import {PackedUserOperation} from "@account-abstraction/contracts/interfaces/PackedUserOperation.sol";
import {MessageHashUtils} from "@openzeppelin/contracts/utils/cryptography/MessageHashUtils.sol";
import {ERC1967Proxy} from "@openzeppelin/contracts/proxy/ERC1967/ERC1967Proxy.sol";
import {SmartAccount} from "../src/SmartAccount.sol";
import {Counter} from "../src/Counter.sol";

contract SmartAccountTest is Test {
    using MessageHashUtils for bytes32;

    // ───────────────────────────── State ───────────────────────────────

    EntryPoint public entryPoint;
    SmartAccount public account;
    Counter public counter;

    address public owner;
    uint256 public ownerKey;
    address public stranger;
    address payable public beneficiary;

    // ───────────────────────────── Setup ───────────────────────────────

    function setUp() public {
        entryPoint = new EntryPoint();
        (owner, ownerKey) = makeAddrAndKey("owner");
        stranger = makeAddr("stranger");

        // Deploy implementation, then an ERC1967Proxy that delegatecalls initialize(owner)
        SmartAccount implementation = new SmartAccount(IEntryPoint(address(entryPoint)));
        bytes memory initData = abi.encodeCall(SmartAccount.initialize, (owner));
        ERC1967Proxy proxy = new ERC1967Proxy(address(implementation), initData);
        account = SmartAccount(payable(address(proxy)));

        // Fund: EntryPoint deposit (gas for UserOps) + direct ETH (for value-forwarding tests)
        entryPoint.depositTo{value: 1 ether}(address(account));
        vm.deal(address(account), 1 ether);

        counter = new Counter();
        beneficiary = payable(makeAddr("beneficiary"));
    }

    // ───────────────────────────── Helpers ─────────────────────────────

    /// @notice Packs two uint128 values into a single bytes32.
    ///         Used for accountGasLimits (verificationGas | callGas)
    ///         and gasFees (maxPriorityFee | maxFee).
    function _packGas(uint256 upper, uint256 lower) internal pure returns (bytes32) {
        return bytes32((upper << 128) | lower);
    }

    /// @notice Builds a PackedUserOperation with sensible gas defaults.
    function _buildUserOp(bytes memory callData) internal view returns (PackedUserOperation memory) {
        return PackedUserOperation({
            sender: address(account),
            nonce: account.getNonce(),
            initCode: "",
            callData: callData,
            accountGasLimits: _packGas(200_000, 200_000),
            preVerificationGas: 50_000,
            gasFees: _packGas(1 gwei, 1 gwei),
            paymasterAndData: "",
            signature: ""
        });
    }

    /// @notice Must be `external` so the struct is passed as calldata —
    ///         getUserOpHash requires calldata, and memory→calldata conversion
    ///         only happens at an external call boundary.
    function getUserOpHash(PackedUserOperation calldata userOp) external view returns (bytes32) {
        return entryPoint.getUserOpHash(userOp);
    }

    /// @notice Signs a UserOp: compute hash → EIP-191 prefix → vm.sign → pack (r, s, v).
    function _signUserOp(
        PackedUserOperation memory userOp,
        uint256 privateKey
    ) internal view returns (bytes memory) {
        bytes32 userOpHash = this.getUserOpHash(userOp);
        bytes32 ethSignedHash = userOpHash.toEthSignedMessageHash();
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(privateKey, ethSignedHash);
        return abi.encodePacked(r, s, v);
    }

    // ────────────────────── Initialization tests ──────────────────────

    function test_initialize_setsOwner() public view {
        assertEq(account.owner(), owner);
    }

    function test_initialize_setsEntryPoint() public view {
        assertEq(address(account.entryPoint()), address(entryPoint));
    }

    function test_initialize_cannotReinitialize() public {
        vm.expectRevert();
        account.initialize(stranger);
    }

    function test_initialize_revertsOnZeroAddress() public {
        SmartAccount impl = new SmartAccount(IEntryPoint(address(entryPoint)));
        bytes memory initData = abi.encodeCall(SmartAccount.initialize, (address(0)));

        vm.expectRevert("invalid owner");
        new ERC1967Proxy(address(impl), initData);
    }

    // ───────────────── Signature validation tests ─────────────────────

    function test_validateUserOp_validOwnerSignature() public {
        PackedUserOperation memory userOp = _buildUserOp("");
        userOp.signature = _signUserOp(userOp, ownerKey);

        // Must pass the real userOpHash — _validateSignature uses it to recover the signer
        bytes32 userOpHash = this.getUserOpHash(userOp);

        vm.prank(address(entryPoint));
        uint256 validationData = account.validateUserOp(userOp, userOpHash, 0);
        assertEq(validationData, 0); // SIG_VALIDATION_SUCCESS
    }

    function test_validateUserOp_wrongSigner() public {
        (, uint256 wrongKey) = makeAddrAndKey("wrong");

        PackedUserOperation memory userOp = _buildUserOp("");
        userOp.signature = _signUserOp(userOp, wrongKey);
        bytes32 userOpHash = this.getUserOpHash(userOp);

        vm.prank(address(entryPoint));
        uint256 validationData = account.validateUserOp(userOp, userOpHash, 0);
        assertEq(validationData, 1); // SIG_VALIDATION_FAILED
    }

    function test_validateUserOp_malformedSignature() public {
        PackedUserOperation memory userOp = _buildUserOp("");
        userOp.signature = hex"dead"; // too short — not a valid 65-byte ECDSA sig
        bytes32 userOpHash = this.getUserOpHash(userOp);

        vm.prank(address(entryPoint));
        uint256 validationData = account.validateUserOp(userOp, userOpHash, 0);
        assertEq(validationData, 1); // SIG_VALIDATION_FAILED, no revert
    }

    function test_validateUserOp_onlyEntryPoint() public {
        PackedUserOperation memory userOp = _buildUserOp("");
        userOp.signature = _signUserOp(userOp, ownerKey);

        vm.prank(stranger);
        vm.expectRevert("account: not from EntryPoint");
        account.validateUserOp(userOp, bytes32(0), 0);
    }

    // ──────────────────────── Execution tests ─────────────────────────

    function test_execute_fromEntryPoint() public {
        // Full ERC-4337 flow: handleOps → validateUserOp → execute → counter.increment
        bytes memory callData = abi.encodeCall(
            SmartAccount.execute,
            (address(counter), 0, abi.encodeCall(Counter.increment, ()))
        );
        PackedUserOperation memory userOp = _buildUserOp(callData);
        userOp.signature = _signUserOp(userOp, ownerKey);

        PackedUserOperation[] memory ops = new PackedUserOperation[](1);
        ops[0] = userOp;
        entryPoint.handleOps(ops, beneficiary);

        assertEq(counter.getCount(address(account)), 1);
    }

    function test_execute_fromOwner() public {
        vm.prank(owner);
        account.execute(address(counter), 0, abi.encodeCall(Counter.increment, ()));
        assertEq(counter.getCount(address(account)), 1);
    }

    function test_execute_revertsFromStranger() public {
        vm.prank(stranger);
        vm.expectRevert(SmartAccount.OnlyOwnerOrEntryPoint.selector);
        account.execute(address(counter), 0, abi.encodeCall(Counter.increment, ()));
    }

    function test_executeBatch_works() public {
        address[] memory targets = new address[](2);
        targets[0] = address(counter);
        targets[1] = address(counter);

        uint256[] memory values = new uint256[](2);

        bytes[] memory calldatas = new bytes[](2);
        calldatas[0] = abi.encodeCall(Counter.increment, ());
        calldatas[1] = abi.encodeCall(Counter.increment, ());

        vm.prank(owner);
        account.executeBatch(targets, values, calldatas);
        assertEq(counter.getCount(address(account)), 2);
    }

    function test_executeBatch_revertsOnLengthMismatch() public {
        address[] memory targets = new address[](2);
        uint256[] memory values = new uint256[](1); // mismatch
        bytes[] memory calldatas = new bytes[](2);

        vm.prank(owner);
        vm.expectRevert("length mismatch");
        account.executeBatch(targets, values, calldatas);
    }

    // ─────────────────────── Receive ETH test ─────────────────────────

    function test_receiveEth() public {
        uint256 balanceBefore = address(account).balance;

        vm.deal(stranger, 1 ether);
        vm.prank(stranger);
        (bool success,) = address(account).call{value: 0.5 ether}("");

        assertTrue(success);
        assertEq(address(account).balance, balanceBefore + 0.5 ether);
    }

    // ───────────────── Session key registration tests ─────────────────

    function test_registerSessionKey_emitsEvent() public {
        (address sessionKey,) = makeAddrAndKey("sessionKey");

        vm.expectEmit(true, false, false, true);
        emit SmartAccount.SessionKeyAdded(sessionKey, 2000);

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(1000),
            uint48(2000)
        );
    }

    function test_registerSessionKey_storesData() public {
        (address sessionKey,) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(1000),
            uint48(2000)
        );

        (address target, bytes4 selector, uint48 validAfter, uint48 validUntil) = account.sessionKeys(sessionKey);
        assertEq(target, address(counter));
        assertEq(selector, Counter.increment.selector);
        assertEq(validAfter, 1000);
        assertEq(validUntil, 2000);
    }

    function test_registerSessionKey_revertsZeroAddress() public {
        vm.prank(owner);
        vm.expectRevert(SmartAccount.InvalidSessionKeyParams.selector);
        account.registerSessionKey(
            address(0),
            address(counter),
            Counter.increment.selector,
            uint48(1000),
            uint48(2000)
        );
    }

    function test_registerSessionKey_revertsZeroTarget() public {
        (address sessionKey,) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        vm.expectRevert(SmartAccount.InvalidSessionKeyParams.selector);
        account.registerSessionKey(
            sessionKey,
            address(0),
            Counter.increment.selector,
            uint48(1000),
            uint48(2000)
        );
    }

    function test_registerSessionKey_revertsZeroSelector() public {
        (address sessionKey,) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        vm.expectRevert(SmartAccount.InvalidSessionKeyParams.selector);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            bytes4(0),
            uint48(1000),
            uint48(2000)
        );
    }

    function test_registerSessionKey_revertsZeroValidUntil() public {
        (address sessionKey,) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        vm.expectRevert(SmartAccount.InvalidSessionKeyParams.selector);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(1000),
            uint48(0)
        );
    }

    function test_registerSessionKey_revertsValidUntilLteValidAfter() public {
        (address sessionKey,) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        vm.expectRevert(SmartAccount.InvalidSessionKeyParams.selector);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(2000),
            uint48(2000) // equal — should fail
        );
    }

    function test_registerSessionKey_revertsDuplicate() public {
        (address sessionKey,) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(1000),
            uint48(2000)
        );

        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSelector(SmartAccount.SessionKeyAlreadyRegistered.selector, sessionKey));
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(1000),
            uint48(2000)
        );
    }

    function test_registerSessionKey_revertsFromStranger() public {
        (address sessionKey,) = makeAddrAndKey("sessionKey");

        vm.prank(stranger);
        vm.expectRevert(SmartAccount.OnlyOwnerOrEntryPoint.selector);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(1000),
            uint48(2000)
        );
    }

    function test_registerSessionKey_viaEntryPoint() public {
        (address sessionKey,) = makeAddrAndKey("sessionKey");

        bytes memory callData = abi.encodeCall(
            SmartAccount.registerSessionKey,
            (sessionKey, address(counter), Counter.increment.selector, uint48(1000), uint48(2000))
        );
        PackedUserOperation memory userOp = _buildUserOp(callData);
        userOp.signature = _signUserOp(userOp, ownerKey);

        PackedUserOperation[] memory ops = new PackedUserOperation[](1);
        ops[0] = userOp;
        entryPoint.handleOps(ops, beneficiary);

        (, , , uint48 validUntil) = account.sessionKeys(sessionKey);
        assertEq(validUntil, 2000);
    }

    // ─────────────────── Session key revocation tests ─────────────────

    function test_revokeSessionKey_emitsEvent() public {
        (address sessionKey,) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(1000),
            uint48(2000)
        );

        vm.expectEmit(true, false, false, false);
        emit SmartAccount.SessionKeyRevoked(sessionKey);

        vm.prank(owner);
        account.revokeSessionKey(sessionKey);
    }

    function test_revokeSessionKey_deletesData() public {
        (address sessionKey,) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(1000),
            uint48(2000)
        );

        vm.prank(owner);
        account.revokeSessionKey(sessionKey);

        (, , , uint48 validUntil) = account.sessionKeys(sessionKey);
        assertEq(validUntil, 0);
    }

    function test_revokeSessionKey_revertsIfNotRegistered() public {
        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSelector(SmartAccount.SessionKeyNotRegistered.selector, stranger));
        account.revokeSessionKey(stranger);
    }

    function test_revokeSessionKey_revertsFromStranger() public {
        (address sessionKey,) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(1000),
            uint48(2000)
        );

        vm.prank(stranger);
        vm.expectRevert(SmartAccount.OnlyOwnerOrEntryPoint.selector);
        account.revokeSessionKey(sessionKey);
    }

    function test_revokeSessionKey_viaEntryPoint() public {
        (address sessionKey,) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(1000),
            uint48(2000)
        );

        bytes memory callData = abi.encodeCall(
            SmartAccount.revokeSessionKey,
            (sessionKey)
        );
        PackedUserOperation memory userOp = _buildUserOp(callData);
        userOp.signature = _signUserOp(userOp, ownerKey);

        PackedUserOperation[] memory ops = new PackedUserOperation[](1);
        ops[0] = userOp;
        entryPoint.handleOps(ops, beneficiary);

        (, , , uint48 validUntil) = account.sessionKeys(sessionKey);
        assertEq(validUntil, 0);
    }

    // ──────────────── Session key validation tests (unit) ─────────────

    function test_validateUserOp_sessionKey_validOp() public {
        (address sessionKey, uint256 sessionPrivKey) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(100),
            uint48(5000)
        );

        bytes memory callData = abi.encodeCall(
            SmartAccount.execute,
            (address(counter), 0, abi.encodeCall(Counter.increment, ()))
        );
        PackedUserOperation memory userOp = _buildUserOp(callData);
        userOp.signature = _signUserOp(userOp, sessionPrivKey);
        bytes32 userOpHash = this.getUserOpHash(userOp);

        vm.prank(address(entryPoint));
        uint256 validationData = account.validateUserOp(userOp, userOpHash, 0);

        assertNotEq(validationData, 0);
        assertNotEq(validationData, 1);

        // Decode: lowest 160 bits = aggregator (0), next 48 bits = validUntil, next 48 bits = validAfter
        uint48 validUntil = uint48(validationData >> 160);
        uint48 validAfter = uint48(validationData >> 208);
        assertEq(validUntil, 5000);
        assertEq(validAfter, 100);
    }

    function test_validateUserOp_sessionKey_wrongTarget() public {
        (address sessionKey, uint256 sessionPrivKey) = makeAddrAndKey("sessionKey");
        address wrongTarget = makeAddr("wrongTarget");

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(100),
            uint48(5000)
        );

        bytes memory callData = abi.encodeCall(
            SmartAccount.execute,
            (wrongTarget, 0, abi.encodeCall(Counter.increment, ()))
        );
        PackedUserOperation memory userOp = _buildUserOp(callData);
        userOp.signature = _signUserOp(userOp, sessionPrivKey);
        bytes32 userOpHash = this.getUserOpHash(userOp);

        vm.prank(address(entryPoint));
        uint256 validationData = account.validateUserOp(userOp, userOpHash, 0);
        assertEq(validationData, 1); // SIG_VALIDATION_FAILED
    }

    function test_validateUserOp_sessionKey_wrongSelector() public {
        (address sessionKey, uint256 sessionPrivKey) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector, // only increment allowed
            uint48(100),
            uint48(5000)
        );

        bytes memory callData = abi.encodeCall(
            SmartAccount.execute,
            (address(counter), 0, abi.encodeCall(Counter.setNumber, (42)))
        );
        PackedUserOperation memory userOp = _buildUserOp(callData);
        userOp.signature = _signUserOp(userOp, sessionPrivKey);
        bytes32 userOpHash = this.getUserOpHash(userOp);

        vm.prank(address(entryPoint));
        uint256 validationData = account.validateUserOp(userOp, userOpHash, 0);
        assertEq(validationData, 1); // SIG_VALIDATION_FAILED
    }

    function test_validateUserOp_sessionKey_executeBatchRejected() public {
        (address sessionKey, uint256 sessionPrivKey) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(100),
            uint48(5000)
        );

        address[] memory targets = new address[](1);
        targets[0] = address(counter);
        uint256[] memory values = new uint256[](1);
        bytes[] memory calldatas = new bytes[](1);
        calldatas[0] = abi.encodeCall(Counter.increment, ());

        bytes memory callData = abi.encodeCall(
            SmartAccount.executeBatch,
            (targets, values, calldatas)
        );
        PackedUserOperation memory userOp = _buildUserOp(callData);
        userOp.signature = _signUserOp(userOp, sessionPrivKey);
        bytes32 userOpHash = this.getUserOpHash(userOp);

        vm.prank(address(entryPoint));
        uint256 validationData = account.validateUserOp(userOp, userOpHash, 0);
        assertEq(validationData, 1); // SIG_VALIDATION_FAILED
    }

    function test_validateUserOp_sessionKey_revokedKeyFails() public {
        (address sessionKey, uint256 sessionPrivKey) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(100),
            uint48(5000)
        );

        vm.prank(owner);
        account.revokeSessionKey(sessionKey);

        bytes memory callData = abi.encodeCall(
            SmartAccount.execute,
            (address(counter), 0, abi.encodeCall(Counter.increment, ()))
        );
        PackedUserOperation memory userOp = _buildUserOp(callData);
        userOp.signature = _signUserOp(userOp, sessionPrivKey);
        bytes32 userOpHash = this.getUserOpHash(userOp);

        vm.prank(address(entryPoint));
        uint256 validationData = account.validateUserOp(userOp, userOpHash, 0);
        assertEq(validationData, 1); // SIG_VALIDATION_FAILED
    }

    function test_validateUserOp_sessionKey_nonCanonicalOffset() public {
        (address sessionKey, uint256 sessionPrivKey) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(100),
            uint48(5000)
        );

        // Craft calldata with non-canonical ABI offset.
        // Normal execute(address,uint256,bytes) has offset 0x60 at [68:100].
        // We set offset to 0xA0, place the allowed selector at [132:136] to
        // fool a naive fixed-offset check, and put the real (malicious) data
        // where Solidity's abi.decode would actually read it.
        bytes memory malicious = abi.encodePacked(
            SmartAccount.execute.selector,     // [0:4]   outer selector
            bytes32(uint256(uint160(address(counter)))), // [4:36]  target
            bytes32(uint256(0)),                // [36:68] value
            bytes32(uint256(0xA0)),             // [68:100] non-canonical offset
            bytes32(0),                         // [100:132] padding
            Counter.increment.selector,         // [132:136] decoy selector
            bytes28(0),                         // [136:164] padding
            bytes32(uint256(4)),                // [164:196] length of inner data
            Counter.setNumber.selector,         // [196:200] actual malicious selector
            bytes28(0)                          // [200:228] padding
        );

        PackedUserOperation memory userOp = _buildUserOp(malicious);
        userOp.signature = _signUserOp(userOp, sessionPrivKey);
        bytes32 userOpHash = this.getUserOpHash(userOp);

        vm.prank(address(entryPoint));
        uint256 validationData = account.validateUserOp(userOp, userOpHash, 0);
        assertEq(validationData, 1); // SIG_VALIDATION_FAILED
    }

    function test_validateUserOp_sessionKey_nonZeroValue() public {
        (address sessionKey, uint256 sessionPrivKey) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(100),
            uint48(5000)
        );

        bytes memory callData = abi.encodeCall(
            SmartAccount.execute,
            (address(counter), 1 ether, abi.encodeCall(Counter.increment, ()))
        );
        PackedUserOperation memory userOp = _buildUserOp(callData);
        userOp.signature = _signUserOp(userOp, sessionPrivKey);
        bytes32 userOpHash = this.getUserOpHash(userOp);

        vm.prank(address(entryPoint));
        uint256 validationData = account.validateUserOp(userOp, userOpHash, 0);
        assertEq(validationData, 1); // SIG_VALIDATION_FAILED
    }

    function test_validateUserOp_sessionKey_zeroDataLength() public {
        (address sessionKey, uint256 sessionPrivKey) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(100),
            uint48(5000)
        );

        // Craft calldata where data.length is 0 but trailing bytes contain the
        // allowed selector at [132:136]. Without the dataLen check, our validation
        // would read the trailing bytes and pass, while execute would forward empty
        // calldata (hitting the target's fallback instead of increment).
        bytes memory malicious = abi.encodePacked(
            SmartAccount.execute.selector,               // [0:4]   outer selector
            bytes32(uint256(uint160(address(counter)))),  // [4:36]  target
            bytes32(uint256(0)),                          // [36:68] value
            bytes32(uint256(0x60)),                       // [68:100] canonical offset
            bytes32(uint256(0)),                          // [100:132] data.length = 0
            Counter.increment.selector,                   // [132:136] trailing decoy
            bytes28(0)                                    // [136:164] padding
        );

        PackedUserOperation memory userOp = _buildUserOp(malicious);
        userOp.signature = _signUserOp(userOp, sessionPrivKey);
        bytes32 userOpHash = this.getUserOpHash(userOp);

        vm.prank(address(entryPoint));
        uint256 validationData = account.validateUserOp(userOp, userOpHash, 0);
        assertEq(validationData, 1); // SIG_VALIDATION_FAILED
    }

    function test_validateUserOp_sessionKey_truncatedCallData() public {
        (address sessionKey, uint256 sessionPrivKey) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(100),
            uint48(5000)
        );

        // Craft calldata that is 100 bytes — long enough to pass a naive >= 100
        // check but too short for the [100:132] slice. Must return
        // SIG_VALIDATION_FAILED, not revert.
        bytes memory truncated = abi.encodePacked(
            SmartAccount.execute.selector,               // [0:4]   outer selector
            bytes32(uint256(uint160(address(counter)))),  // [4:36]  target
            bytes32(uint256(0)),                          // [36:68] value
            bytes32(uint256(0x60))                        // [68:100] canonical offset
            // nothing beyond 100 — triggers OOB without the length fix
        );

        PackedUserOperation memory userOp = _buildUserOp(truncated);
        userOp.signature = _signUserOp(userOp, sessionPrivKey);
        bytes32 userOpHash = this.getUserOpHash(userOp);

        vm.prank(address(entryPoint));
        uint256 validationData = account.validateUserOp(userOp, userOpHash, 0);
        assertEq(validationData, 1); // SIG_VALIDATION_FAILED, no revert
    }

    function test_validateUserOp_sessionKey_hugeDataLen() public {
        (address sessionKey, uint256 sessionPrivKey) = makeAddrAndKey("sessionKey");

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            uint48(100),
            uint48(5000)
        );

        // Craft calldata with a huge data.length that would overflow 132 + dataLen.
        // Must return SIG_VALIDATION_FAILED, not revert with arithmetic overflow.
        bytes memory malicious = abi.encodePacked(
            SmartAccount.execute.selector,               // [0:4]   outer selector
            bytes32(uint256(uint160(address(counter)))),  // [4:36]  target
            bytes32(uint256(0)),                          // [36:68] value
            bytes32(uint256(0x60)),                       // [68:100] canonical offset
            bytes32(type(uint256).max),                   // [100:132] huge dataLen
            Counter.increment.selector,                   // [132:136] inner selector
            bytes28(0)                                    // [136:164] padding
        );

        PackedUserOperation memory userOp = _buildUserOp(malicious);
        userOp.signature = _signUserOp(userOp, sessionPrivKey);
        bytes32 userOpHash = this.getUserOpHash(userOp);

        vm.prank(address(entryPoint));
        uint256 validationData = account.validateUserOp(userOp, userOpHash, 0);
        assertEq(validationData, 1); // SIG_VALIDATION_FAILED, no revert
    }

    // ──────────── Session key full EntryPoint flow test ───────────────

    function test_execute_sessionKey_fullFlow() public {
        (address sessionKey, uint256 sessionPrivKey) = makeAddrAndKey("sessionKey");

        uint48 validAfter = uint48(block.timestamp);
        uint48 validUntil = uint48(block.timestamp + 1 hours);

        vm.prank(owner);
        account.registerSessionKey(
            sessionKey,
            address(counter),
            Counter.increment.selector,
            validAfter,
            validUntil
        );

        bytes memory callData = abi.encodeCall(
            SmartAccount.execute,
            (address(counter), 0, abi.encodeCall(Counter.increment, ()))
        );
        PackedUserOperation memory userOp = _buildUserOp(callData);
        userOp.signature = _signUserOp(userOp, sessionPrivKey);

        PackedUserOperation[] memory ops = new PackedUserOperation[](1);
        ops[0] = userOp;
        entryPoint.handleOps(ops, beneficiary);

        assertEq(counter.getCount(address(account)), 1);
    }
}
