// SPDX-License-Identifier: MIT
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

        assertEq(counter.number(), 1);
    }

    function test_execute_fromOwner() public {
        vm.prank(owner);
        account.execute(address(counter), 0, abi.encodeCall(Counter.increment, ()));
        assertEq(counter.number(), 1);
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
        assertEq(counter.number(), 2);
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
}
